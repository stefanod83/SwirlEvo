# Stack Versioning

Every compose stack in Swirl keeps a rolling **history of its
content** — the authored `docker-compose.yml` plus its `.env`
file. The history is populated automatically on Save, browsable
from the stack editor's **History** dropdown, and any prior
revision can be **diffed** against the current state or **restored**
with a single click.

The feature is deliberately focused on *authored content*: status,
host, name and deploy timestamps are **not** snapshot. Restore
writes only `Content` + `EnvFile` back — a restore never moves a
stack or tears down running containers on its own.

---

## Data model

Entity `dao.ComposeStackVersion` (DAO `ComposeStackVersion{Create,
List, Get, Prune}`, both BoltDB and MongoDB backends):

| Field | Type | Meaning |
|---|---|---|
| `id` | string | PK |
| `stackId` | string | Foreign key to `ComposeStack` |
| `revision` | int | Monotonic per-stack counter, 1-based |
| `content` | string | The YAML **as it was before the change** |
| `envFile` | string | The `.env` **as it was before the change** |
| `reason` | string | See [Reason taxonomy](#reason-taxonomy) below |
| `createdAt` | time | Snapshot creation time |
| `createdBy` | operator | User who triggered the change |

The "before the change" detail is crucial: `snapshotIfChanged` is
called on the Save path **before** the mutating `ComposeStackUpdate`,
with `prev = the DB row as it stands now`, `next = the incoming
payload`. If the two differ on `Content` or `EnvFile`, a snapshot
of `prev` is recorded.

### Retention

Hardcoded at **20 entries per stack**
(`biz/compose_stack_version.go::defaultStackVersionRetention`).
Older revisions are pruned best-effort after every successful
snapshot (`ComposeStackVersionPrune`). The cap is a pragmatic
balance between usefulness (a month of daily edits) and storage —
BoltDB stays compact.

Prune failures are swallowed (logged, not propagated): a slightly
overgrown history is strictly better than a failed Save.

---

## Reason taxonomy

The `reason` field identifies *what kind of change* the snapshot
reflects. It is the differentiator the History UI surfaces so
operators can skim the list without scrolling the diff:

| Reason | Emitted by |
|---|---|
| `save` | Plain Save flow — the operator edited the YAML / envFile in the editor |
| `addon-inject` | Save flow that included an addon-wizard mutation (labels rewritten by `injectAddonLabels`) |
| `restore:rev<N>` | Automatic snapshot taken **before** applying `RestoreVersion(versionID=<N>)` so the restore itself is reversible |

Hand-crafted marker — the string is stored verbatim, the UI parses
the prefix to pick an icon.

---

## Save-path behaviour

`biz.composeStackBiz.Save` (and the `SaveWithAddons` wrapper)
invokes `snapshotIfChanged(prev, next, reason, user)`:

```go
func (b *composeStackBiz) snapshotIfChanged(
    ctx context.Context,
    next *dao.ComposeStack,
    reason string,
    user web.User,
) {
    prev, _ := b.di.ComposeStackGet(ctx, next.ID)
    if prev == nil {
        return // no-op on create
    }
    if prev.Content == next.Content && prev.EnvFile == next.EnvFile {
        return // no-op on unchanged save
    }
    // compute next revision number, record prev content + envFile,
    // prune beyond retention — all best-effort
}
```

Two hard rules:

1. **No snapshot on first Save**: a create has no prior state to
   preserve. The first Save is always revision-less.
2. **No snapshot on unchanged Save**: re-saving an identical
   payload (metadata ping, stale form submit) does not grow the
   history. Both `Content` and `EnvFile` must match verbatim for
   the Save to be considered a no-op.

Errors inside `snapshotIfChanged` never propagate — the Save the
operator asked for is honoured even if the version bucket is
momentarily unavailable.

---

## Restore

`RestoreVersion(stackID, versionID)` loads the target snapshot and:

1. Refuses if the snapshot's `StackID` doesn't match (cross-stack
   protection).
2. **No-op** if `current.Content == version.Content &&
   current.EnvFile == version.EnvFile` — restoring to an identical
   state would spam the history with empty snapshots.
3. Calls `snapshotIfChanged` on the current state with
   `reason = "restore:rev<N>"` so the restore is reversible. The
   snapshot bytes are the ones the operator is about to lose.
4. Writes `current.Content = version.Content` and
   `current.EnvFile = version.EnvFile`, updates `UpdatedAt` /
   `UpdatedBy`, and persists. **Nothing else is touched** — not
   `Status`, not `HostID`, not `Name`, not deploy timestamps.
5. Emits `stack.update` in the event log.

The restored stack is **inactive-equivalent for deploy purposes**:
the authored content has changed, but containers on the host still
reflect the previous Deploy. The operator clicks Deploy (or
Start, which triggers Deploy post-Stop) to apply the restored
content to the host.

---

## API

Defined on `ComposeStackHandler` (`api/compose_stack.go`):

| Path | Method | Auth | Purpose |
|---|---|---|---|
| `/api/compose-stack/versions` | GET | `stack.view` | List snapshots for a stack (`?stackId=…`). Response **strips** `content` + `envFile` to keep payloads cheap — the list is just metadata |
| `/api/compose-stack/version-get` | GET | `stack.view` | Fetch a single snapshot **including** body (for the diff modal) |
| `/api/compose-stack/version-restore` | POST | `stack.edit` | Restore `versionId` onto `stackId` |

The List response includes for each entry: `id`, `stackId`,
`revision`, `reason`, `createdAt`, `createdBy`. The consumer
fetches `content` + `envFile` only when the user actually opens
the diff modal.

---

## UI

Component: `ui/src/components/stack-version/StackVersionHistory.vue`.

### History dropdown

Located in the stack editor header. Each row shows:
- Revision number (`#5`).
- Reason (icon + short label).
- `createdAt` relative time + absolute tooltip.
- `createdBy` operator name.

Clicking a row opens the **diff modal**.

### Diff modal

Side-by-side CodeMirror panes:
- **Left**: the snapshot (`version.Content` / `version.EnvFile`).
- **Right**: the current stack state.

A tab switcher above the panes toggles between `docker-compose.yml`
and `.env`. The modal offers two actions:

- **Copy** — copy the snapshot content to clipboard (for manual
  partial reuse).
- **Restore this revision** — confirm dialog → `POST
  /version-restore`. On success the editor reloads and the history
  dropdown shows a new `restore:rev<N>` entry at the top.

---

## Edge cases & limitations

- **History lives only on managed stacks** — external (CLI-authored,
  discovered) stacks have no persisted content, so no history. The
  Import flow creates revision 1 from the reconstructed YAML at
  import time, which then becomes the baseline for future history.
- **Retention is hardcoded** at 20. There is no per-stack or
  per-tenant override yet. A future enhancement may move the cap
  into Setting.
- **No cross-stack copy** — restoring a revision requires the
  snapshot's `StackID` to match the target. There's no "apply
  stack A's revision 3 onto stack B" path (intentional; use
  Migrate for that).
- **Snapshot size** = the full content + env blob. Large YAMLs (a
  few hundred services) multiply by 20 — for BoltDB operators on
  tight disks, watch the `compose_stack_version` bucket size and
  lower the retention constant if needed.

---

## File reference

- `biz/compose_stack_version.go` — `snapshotIfChanged`,
  `ListVersions`, `GetVersion`, `RestoreVersion`.
- `dao/entity.go::ComposeStackVersion` — entity definition.
- `dao/dao.go::ComposeStackVersion{Create,List,Get,Prune}` — DAO
  interface.
- `dao/bolt/*`, `dao/mongo/*` — backend implementations.
- `api/compose_stack.go` — `Versions`, `VersionGet`,
  `VersionRestore` handlers.
- `ui/src/components/stack-version/StackVersionHistory.vue` —
  dropdown + diff modal.
- `ui/src/pages/compose_stack/Edit.vue` — integration point.
