# HashiCorp Vault integration

Swirl integrates with [HashiCorp Vault](https://www.vaultproject.io/) to:

1. **Source the backup encryption key** (`SWIRL_BACKUP_KEY`) from a KVv2 entry instead of a static env var.
2. **Maintain a catalog of `VaultSecret` references** (pointers, never values) that operators can curate from the UI.
3. **Materialise per-stack secrets** inside standalone compose stacks at deploy time, with three injection modes: `tmpfs`, `volume`, `init`, plus `env` for environment variables.
4. **Detect drift**: compare the SHA-256 of the value materialised at last deploy with the value currently in Vault, and surface a badge per binding when the two differ.

Swirl never persists raw secret values. The catalog stores only the path to a KVv2 entry; values are fetched on demand at deploy time and never written to a database, log, or backup archive.

---

## Prerequisites

- A reachable Vault server (OSS or Enterprise).
- A KVv2 mount enabled (default mount name `secret`):
  ```bash
  vault secrets enable -path=secret -version=2 kv
  ```
- A token or AppRole pair with `read` permission on the subtree Swirl will use:
  ```hcl
  # vault policy: swirl-read
  path "secret/data/swirl/*" {
    capabilities = ["read"]
  }
  path "secret/metadata/swirl/*" {
    capabilities = ["read", "list"]
  }
  ```
  Then either:
  ```bash
  vault token create -policy=swirl-read -ttl=720h
  # or for AppRole
  vault auth enable approle
  vault write auth/approle/role/swirl token_policies=swirl-read
  vault read   auth/approle/role/swirl/role-id
  vault write -f auth/approle/role/swirl/secret-id
  ```

For Enterprise: also set the `X-Vault-Namespace` header. Swirl exposes this as the `namespace` setting.

---

## Configuration

All Vault settings live under the **Settings → Vault** panel in the UI. They are stored in the `setting` table (id `vault`) and exposed as `misc.Setting.Vault` to the backend.

| Field | Description | Default |
|---|---|---|
| `enabled` | Master toggle. When `false`, no Vault calls are made. | `false` |
| `address` | Full base URL, e.g. `https://vault.example.com:8200`. | — |
| `namespace` | Enterprise namespace header, e.g. `admin/team-a`. | empty |
| `auth_method` | `token` or `approle`. | `token` |
| `token` | Static token (used when `auth_method=token`). Trimmed of whitespace. | — |
| `approle_path` | Mount path for AppRole, e.g. `approle`. | `approle` |
| `role_id` | AppRole role-id. | — |
| `secret_id` | AppRole secret-id. | — |
| `kv_mount` | KVv2 mount name. | `secret` |
| `kv_prefix` | Logical prefix prepended to every catalog path, e.g. `swirl/`. | empty |
| `backup_key_path` | Logical path of the entry that holds the backup key. | `backup-key` |
| `backup_key_field` | Field name inside that entry. | `value` |
| `default_storage_mode` | Default storage mode for new bindings (`tmpfs` / `volume` / `init`). | `tmpfs` |
| `tls_skip_verify` | Skip TLS verification (testing only). | `false` |
| `ca_cert` | PEM-encoded CA certificate to trust. | empty |
| `request_timeout` | Per-request timeout in seconds. | `10` |

**Important:** Saving the form via the **Save** button persists *and* refreshes the in-memory snapshot, so the **Test connection** button immediately exercises the values you just typed (no restart required).

---

## Auth methods

### Token

Simplest path. Paste the token in the **Token** field. The token must have `read` (and `list` on metadata if you intend to use the **Preview** action) on the subtree under `<kv_mount>/data/<kv_prefix>/`.

### AppRole

Recommended for production. Swirl exchanges `role_id`+`secret_id` for a short-lived client token automatically and caches it for the lease duration.

```bash
vault auth enable approle
vault write auth/approle/role/swirl \
    token_policies=swirl-read \
    token_ttl=1h \
    token_max_ttl=24h \
    secret_id_ttl=0
ROLE_ID=$(vault read -field=role_id auth/approle/role/swirl/role-id)
SECRET_ID=$(vault write -f -field=secret_id auth/approle/role/swirl/secret-id)
```

Paste the two values into Swirl's UI and choose **AppRole**.

---

## `SWIRL_BACKUP_KEY` from Vault

When the `SWIRL_BACKUP_KEY` environment variable is empty (or shorter than 16 chars), Swirl asks the configured Vault provider for the backup key. The lookup goes to:

```
<address>/v1/<kv_mount>/data/<kv_prefix><backup_key_path>
→ extract field <backup_key_field>
```

Example: with `kv_mount=secret`, `kv_prefix=swirl/`, `backup_key_path=backup-key`, `backup_key_field=value`, Swirl reads `secret/data/swirl/backup-key` and uses `data.value`.

```bash
vault kv put secret/swirl/backup-key value="$(openssl rand -base64 32)"
```

The fetched value is cached in memory for **5 minutes** to amortise scryptKDF and Vault round-trips. Rotating the secret in Vault becomes effective within that window — or sooner if Swirl restarts.

When Vault rotates the underlying secret, **previously created backups remain encrypted with the old key**. See [`docs/backup.md`](backup.md) for the recovery flow.

---

## VaultSecret catalog

`VaultSecret` is a *reference* to a KVv2 entry, never a copy. CRUD lives at `Vault Secrets` in the navigation menu and is gated by `vault_secret.{view,edit,delete}` permissions.

Fields:

| Field | Required | Notes |
|---|---|---|
| `name` | yes | Unique identifier inside Swirl, e.g. `db-password`. Letters, digits, dot, underscore, dash. No slashes. |
| `path` | yes | Sub-path under `<kv_prefix>`. Use `myapp/db` to read `<kv_mount>/data/<kv_prefix>myapp/db`. |
| `field` | optional | Selects a single field of the KVv2 entry. Empty → returns the full JSON blob. |
| `desc` | optional | Free-text description. |
| `labels` | optional | Key/value tags for filtering. |

The **Preview** action runs a `read` against Vault and returns *only the field names* (never values). Use it to confirm the entry exists and you have the right keys.

---

## Per-stack bindings (standalone compose stacks)

Per-stack bindings turn a `VaultSecret` reference into something the container can actually consume — either a file on disk inside the container, or an environment variable. Swirl injects the value at deploy time and tracks the SHA-256 hash for drift detection.

### Where to find the UI

Open any standalone stack in **Standalone → Stacks → Edit**. After saving the stack (so it has an ID), a panel **Vault secret bindings** appears at the bottom. Click **Add** to create a binding.

### Lifecycle

The binding integrates into the standalone deploy engine (`docker/compose/standalone.go`) via a four-method hook:

```
BeforeDeploy()    → per-binding setup (volume create + helper container for volume/init)
ApplyToService()  → mutate env / mounts before ContainerCreate
AfterCreate()     → CopyToContainer for tmpfs files, then markDeployed (records hash)
AfterRemove()     → cleanup (helper containers + secret volumes by label)
```

Hooks are best-effort on remove: if Vault is unreachable, stack teardown still works because cleanup uses Docker labels (`com.swirl.compose.secret-stack`, `com.swirl.compose.secret-binding`), not Vault.

### Storage modes

#### `tmpfs` — in-memory, host (default)

Best for ephemeral secrets. The value never touches the host's disk: Swirl mounts a tmpfs over the parent directory and writes the file via `CopyToContainer` between Create and Start, so the bytes only live in the container's memory namespace.

```yaml
# compose.yml
services:
  app:
    image: nginx:alpine
    # No secret declaration here — Swirl injects it.
```

Binding:

| Field | Value |
|---|---|
| Vault secret | `db-password` (catalog name) |
| Service | empty (= apply to all services in the stack) |
| Target | File |
| Target path | `/run/secrets/db_password` |
| Storage mode | `tmpfs` |
| UID / GID / Mode | `0 / 0 / 0400` |

The container sees `cat /run/secrets/db_password` succeed. Multiple bindings sharing the same parent dir collapse to one tmpfs mount (so `/run/secrets/a` + `/run/secrets/b` use one tmpfs, not two).

#### `volume` — named Docker volume

Best when the file must survive container restarts (e.g. inside a worker that re-execs). Swirl creates `<project>_secret_<bindingID>` and uses a short-lived `busybox` helper container to populate it via `CopyToContainer`. The helper is removed immediately after the copy; the volume persists.

```yaml
services:
  worker:
    image: my-worker
    # Swirl will inject /etc/secrets/api-token via a named volume.
```

Binding mirrors the tmpfs example with **Storage mode = `volume`**. Cleanup happens automatically on `Remove` via volume labels.

#### `init` — helper container persists

Same as `volume` but the helper container is **not removed** after the copy — it stays as an exited container in the project. Useful for audit/ops to confirm the init step actually ran. Cleanup is identical: project teardown removes both helper and volume.

#### `env` — environment variable

For tools that read secrets from env:

| Field | Value |
|---|---|
| Vault secret | `db-password` |
| Target | Env |
| Variable name | `DB_PASSWORD` |

Swirl appends `DB_PASSWORD=<value>` to the service env before `ContainerCreate`. The value is visible to anything that can `cat /proc/<pid>/environ` inside the container (so `tmpfs` files are usually preferable for high-sensitivity values).

---

## Drift check

Each binding stores a `DeployedHash` (SHA-256 of the value materialised at last deploy) and `DeployedAt` timestamp. The **Drift check** compares `DeployedHash` against the SHA-256 of the value currently in Vault:

| State | Meaning | UI badge |
|---|---|---|
| `ok` | Hashes match | none |
| `drifted` | Vault value changed since the deploy | **drift** (orange) |
| `missing` | Catalog entry no longer in Vault | **missing** (red) |
| `error` | Vault unreachable / TLS / auth error | **vault error** (red, tooltip with detail) |
| `unknown` | Never deployed yet (`DeployedHash` empty) | none |

The check runs on demand when the bindings panel loads (`/api/compose-stack-secret/drift?stackId=...`), is read-only, and tolerates per-binding failures: one Vault error does not abort the rest.

---

## Troubleshooting

### "Vault connection failed" with no detail

Make sure you're on Swirl ≥ 2.0.0rc1 — older builds dropped the backend's error message in transit. The current build surfaces the exact reason after the bracketed stage tag, e.g. `[auth] token lookup failed: http 403`.

### "vault token is empty" in the test result

The token field is treated as a password and not echoed back on GET. After save+test, paste the value again if the field shows empty.

### "approle login: http 400 invalid role or secret ID"

Verify with curl using the same `address`+`approle_path`:
```bash
curl --request POST --data '{"role_id":"...","secret_id":"..."}' \
  https://vault.example.com:8200/v1/auth/approle/login
```

### TLS issues against an internal CA

Paste the CA's PEM into **CA certificate (PEM)**. Avoid `tls_skip_verify` outside dev.

### "no entry found at this Vault path"

Triggered by the **Preview** action. Confirm with:
```bash
vault kv get secret/swirl/<path>
```
If KVv1 is in use, switch to KVv2 (`kv_mount` v2) — Swirl reads via `/v1/<mount>/data/<path>`.

### Permanent drift on every check

Either (a) something else is mutating the Vault entry (CI, another operator), or (b) the value is being stored differently from what Swirl reads (e.g. KV `value` field vs whole JSON). Set the binding's `field` selector explicitly so Swirl picks the same byte-string each time.

---

## References

- [`docs/backup.md`](backup.md) — backup encryption + key rotation/recovery flow
- `vault/client.go` — HTTP client, KVv2 read, auth (token + AppRole)
- `vault/backup_provider.go` — `SWIRL_BACKUP_KEY` fallback
- `biz/vault_secret.go` — catalog CRUD + Preview
- `biz/compose_stack_secret.go` — per-stack bindings + materializer hook + drift check
- `docker/compose/standalone.go` — DeployHook integration in the standalone engine
