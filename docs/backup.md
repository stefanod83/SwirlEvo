# Backup & restore

Swirl has a built-in backup subsystem that snapshots its own database (settings, roles, users, registries, stacks, hosts, charts, vault secret references, compose-stack secret bindings, and optionally events) into encrypted archives, on a schedule or on demand. Restore is component-selective: pick which slices of the document to overwrite.

This doc covers storage, encryption, schedules, restore, download, and the **key compatibility & recovery** flow that the v2.0.0rc1 release introduced.

---

## Storage

Two backends, selectable from **Settings → Backup storage**. The
AES-256-GCM at-rest format is the same in both — only the
destination of the encrypted bytes changes.

### Filesystem (default)

| Aspect | Default | Override |
|---|---|---|
| Backup directory | `/data/swirl/backups` | env `SWIRL_BACKUP_DIR` |
| File extension | `.swb` (at-rest) / `.enc` (portable) | — |
| File mode | `0600` | — |
| Directory mode | `0750` (created on first write) | — |
| Atomicity | temp file + `rename(2)` | — |

Each backup is a single self-contained file; metadata lives in the
`backup` table (DAO). The on-disk filename is `<8-char id>.swb`,
the human-readable name (`manual-2026-04-16T15-30-45Z`) is metadata
only.

### HashiCorp Vault (KVv2, opt-in)

Enable via `Settings → Backup storage` → `storage_mode = vault`.
Also configure the `vault_prefix` (default `backups`) — it's
appended to the Vault settings' `kv_prefix`.

Archives land at:

```
<kv_mount>/data/<kv_prefix><vault_prefix>/<id>
```

Each KVv2 entry stores two fields:
- `archive` — base64-encoded ciphertext (same AES-256-GCM format
  as the filesystem `.swb`).
- `created_at` — RFC-3339 timestamp.

The storage is *transparent*: `dao.Backup.Path` carries a schema
prefix (`vault:<logical-path>` vs `file://<fs-path>`) and the biz
layer dispatches read/write/delete on that prefix. Rows predating
the schema are treated as filesystem for backward compatibility.

**Required Vault policy** (add to the Swirl role when using this mode):

```hcl
path "<mount>/data/<prefix>/backups/*"     { capabilities = ["create","update","read","delete"] }
path "<mount>/metadata/<prefix>/backups/*" { capabilities = ["read","list","delete"] }
```

**Trade-offs versus filesystem**:

| Aspect | Filesystem | Vault |
|---|---|---|
| Atomic write | Yes (`rename(2)`) | No (single HTTP POST) |
| Size limit | None | **1 MiB default per entry** (Vault-configurable) |
| Retention | File-level (orphaned rows possible on manual `rm`) | Vault version history + explicit delete |
| Off-host durability | Requires separate backup-of-backups | Inherits Vault's HA / replication |
| Network dependency | None | Every backup read/write hits Vault |

**No automatic migration** between modes. Switching the toggle only
affects *new* backups. Old ones stay on whichever backend they were
created in (the schema prefix in `Path` makes this transparent). To
consolidate, create fresh backups under the new mode and delete the
old ones.

---

## Encryption

### Algorithm

- **Cipher:** AES-256-GCM (authenticated encryption with associated data).
- **Nonce:** 12 bytes, freshly random per file.
- **Key:** 32 bytes derived from a passphrase via **scrypt** (`N=32768, r=8, p=1`).

### At-rest format (`.swb`)

```
+--------+--------------+----------------------+
| MAGIC  | NONCE (12 B) | CIPHERTEXT + TAG (n) |
| "SWBR" |              |                      |
+--------+--------------+----------------------+
```

KDF salt is the fixed string `swirl-backup-at-rest`. This makes it possible to derive the same key from the same passphrase deterministically across restarts, at the cost of being unable to use the same passphrase on different installs without producing identical KDF output. That's the whole point: at-rest archives are tied to *this* Swirl instance's `SWIRL_BACKUP_KEY`.

### Portable format (`.enc`)

```
+--------+--------------+--------------+----------------------+
| MAGIC  | SALT (16 B)  | NONCE (12 B) | CIPHERTEXT + TAG (n) |
| "SWBP" |              |              |                      |
+--------+--------------+--------------+----------------------+
```

Random salt → the same passphrase produces a different key on each export. Used when you want to share an archive with another instance: the recipient needs only the passphrase you used at export time, not your `SWIRL_BACKUP_KEY`.

### Where the master key comes from

Source order, first hit wins:

1. `SWIRL_BACKUP_KEY` environment variable (must be ≥ 16 characters).
2. In-process cache (5-minute TTL) of the last Vault response.
3. Vault provider lookup at `<kv_mount>/data/<kv_prefix><backup_key_path>`, field `<backup_key_field>` — see [`docs/vault.md`](vault.md) for setup.

If none of the three yields a key, scheduled backups are skipped (with one warning at startup) and manual backup creation returns `SWIRL_BACKUP_KEY is not configured`.

### Why scryptKDF with a fixed salt?

The fixed salt means we don't need a per-file salt for at-rest archives — every file in the same directory shares the same derived key, so deriving once and caching is enough for the scheduler. The trade-off (no per-file salt diversification) is acceptable here: we control both encrypt and decrypt sides, and the threat model is "what an attacker who reads the disk can do without the env var" — for that, AES-GCM authentication + the env-var requirement is sufficient.

---

## Components

The plaintext document (compressed with gzip before encryption) carries:

- `settings`
- `roles`
- `users` (full export including password hashes + salts)
- `registries`
- `stacks` (Swarm stacks)
- `composeStacks` (standalone)
- `hosts`
- `charts`
- `vaultSecrets` — references only, never values
- `composeStackSecretBindings` — references only, never values
- `events` (audit log; opt-in on restore)

Component selection on restore is checkbox-based (see UI section). Restore order respects dependencies: roles before users, vault secrets before bindings, etc.

---

## Schedules

Three schedule types, at most one row each:

- `daily` — pick the days of the week (e.g. Mon–Fri) and a `HH:MM` time
- `weekly` — pick a single day of the week + time
- `monthly` — pick a day of the month (1–28) + time

The scheduler ticks every hour and runs each schedule at most once per day. Each schedule has a `retention` value (`0` = unlimited): after a successful run, archives older than the N most recent for that source are deleted via the standard `Delete` flow (file removal + DAO row removal).

---

## Restore

### From a stored backup (Restore button)

```
Step 1 → confirm warning ("this will overwrite existing data")
Step 2 → choose components (events is opt-in)
Step 3 → final confirmation
```

The flow is:

```go
// biz/backup.go
raw      := os.ReadFile(rec.Path)
plain    := decryptAtRest(raw)        // uses current master key
doc      := unmarshalGzip(plain)
counts   := importDocument(ctx, doc, components)
```

If `decryptAtRest` returns `errDecrypt`, the master key is wrong. See [Key recovery](#key-compatibility--recovery) below.

### From an uploaded file (Restore from file)

Same as above but accepts both `.swb` (master-key) and `.enc` (passphrase). Auto-detects via the magic bytes. The Preview step decodes only the manifest (counts) so the operator can confirm before committing.

---

## Download

Two modes selectable at download time:

- **Raw** — streams the `.swb` as-is. The recipient needs the *same* `SWIRL_BACKUP_KEY` (or Vault entry) to decrypt.
- **Portable** — decrypts at-rest, then re-encrypts under a fresh passphrase you type into the dialog. Returns a `.enc`. Use this to share a backup outside Swirl.

---

## Key compatibility & recovery

`SWIRL_BACKUP_KEY` rotation is operationally important and breaks at-rest backups silently — a rotated key cannot decrypt archives encrypted under the previous key. Swirl mitigates this with two mechanisms.

### 1. Per-backup key fingerprint

Every new backup record stores a 16-byte HMAC-SHA-256 fingerprint of the master key it was encrypted with (label `swirl-backup-key-fp/v1`). The fingerprint is one-way (you cannot derive the key from it) and identical across two Swirl instances using the same `SWIRL_BACKUP_KEY` (useful for failover diagnostics).

### 2. Startup compatibility check

When the scheduler starts, a non-blocking goroutine compares each backup's stored fingerprint against the fingerprint of the *current* master key. The result is one log line:

```
INFO  backup key check: 12/12 compatible (0 legacy unverified)
WARN  backup key check: 3 incompatible / 1 legacy unverified out of 5 (key fingerprint b2a4…). Use 'Recover' on the Backups page to re-encrypt with the current key.
```

The check **does not** trial-decrypt legacy archives (those that have no stored fingerprint). They appear in the UI as `unverified` until the operator clicks **Verify**, which performs an on-demand trial decrypt:
- success → backfill the fingerprint, mark `compatible`
- failure → mark `incompatible` (no persisted fingerprint — a recovery is still possible)

### 3. The Recover action

For backups in `incompatible` (or `unverified`) state, the **Recover** button opens a dialog asking for the **old `SWIRL_BACKUP_KEY`**. Swirl:

1. Derives the old key via the same `scryptKDF(passphrase, "swirl-backup-at-rest")`.
2. Decrypts the archive with the old key (`decryptAtRestWithKey`).
3. Re-encrypts the same plaintext with the *current* master key (`encryptAtRest`).
4. Atomically replaces the file (`writeFileAtomic` → temp + rename) and updates the DAO row with the new size, fingerprint, and `verified_at` timestamp.

If the supplied passphrase is wrong, the API returns **HTTP 401** with a clear message — nothing is written to disk.

### Concurrency

A per-backup mutex serialises `Recover` against `Delete` on the same ID, so a recovery cannot resurrect a file that another goroutine has just removed. `Restore` and `Download` do not take the lock — atomic rename guarantees they see either the old or the new bytes, never a torn write.

### Permission

Recovery requires the new permission **`backup.recover`** (bit `1 << 13` in `security/perm.go`). It is intentionally separate from:
- `backup.restore` — data-destructive (overwrites the running database)
- `backup.edit` — creates new backups with the *current* key

A holder of `backup.recover` can re-encrypt an archive but cannot, on its own, restore it. Admins always have all permissions; non-admin operators must be granted `backup.recover` explicitly.

### What about portable archives?

The recovery flow targets the at-rest format only. A `.enc` portable archive has its own embedded salt and is decrypted with the passphrase chosen at export time — no compatibility issue arises, because the recipient supplies the passphrase directly via the **Restore from file** flow.

---

## Reference: API endpoints

| Method | Path | Permission | Purpose |
|---|---|---|---|
| GET | `/backup/search` | `backup.view` | List all backups (decorated with `keyStatus`) |
| GET | `/backup/find?id=…` | `backup.view` | Single backup metadata |
| GET | `/backup/status` | `backup.view` | `keyConfigured` summary |
| GET | `/backup/key-status` | `backup.view` | Aggregate compatibility summary + current fingerprint |
| POST | `/backup/create` | `backup.edit` | Manual backup |
| POST | `/backup/delete` | `backup.delete` | Remove archive + metadata |
| POST | `/backup/download` | `backup.download` | Raw or portable export |
| POST | `/backup/restore` | `backup.restore` | Restore from stored archive |
| POST | `/backup/preview` | `backup.restore` | Preview uploaded archive |
| POST | `/backup/upload` | `backup.restore` | Restore from uploaded file |
| POST | `/backup/verify` | `backup.view` | Re-probe one backup against current key |
| POST | `/backup/recover` | `backup.recover` | Re-encrypt with current key using old passphrase |
| GET | `/backup/schedules` | `backup.view` | List schedules |
| POST | `/backup/schedule/save` | `backup.edit` | Create/update a schedule |
| POST | `/backup/schedule/delete` | `backup.edit` | Delete a schedule |

---

## References

- [`docs/vault.md`](vault.md) — Vault setup and `SWIRL_BACKUP_KEY` source
- `biz/backup.go` — main biz, retention, restore
- `biz/backup_crypto.go` — AES-GCM, scryptKDF, fingerprint helpers
- `backup/backup.go` — scheduler + startup compatibility check
- `api/backup.go` — HTTP handlers
- `ui/src/pages/backup/List.vue` — operator UI
