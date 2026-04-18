# CHANGELOG

## v2.0.0rc1 (2025) — SwirlEvo

First release of the SwirlEvo fork (continues [cuigh/swirl](https://github.com/cuigh/swirl)).

> Standalone-host management is the headline feature. Swarm mode is preserved
> and unchanged at the user level; flip with `MODE=standalone`. Two-database
> backends (MongoDB / BoltDB) supported.

### Dependencies & build

* **Go 1.22 → 1.25** — required by Docker SDK v28's transitive
  `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` (≥ Go 1.25).
  `exclude` directive on the obsolete `google.golang.org/genproto` monolithic
  module to avoid ambiguous imports with split sub-modules.
* **Docker SDK v20 → v28**. Type migrations applied across `docker/`, `biz/`,
  `api/`: `container.Summary`, `container.RestartPolicyMode`,
  `image.InspectResponse`, `network.Inspect`, `volume.Volume`,
  `swarm.ServiceCreateResponse / ServiceUpdateResponse`,
  `registry.AuthConfig` (was `types.AuthConfig`),
  `network.EnableIPv6` is now `*bool`. `BaseLayer` removed from
  `image.RootFS`. `WithAPIVersionNegotiation()` is now the default for both
  primary and per-host clients.
* **MongoDB driver 1.8 → 1.17**. `ioutil` → `os` migration.
* **Frontend**: Vue 3.5, Vite 5.4, TypeScript 5.5, Naive-UI 2.40,
  vue-i18n v11, `xterm` → `@xterm/xterm` (new namespace).
* **`.dockerignore`** excludes `ui/node_modules`, `ui/dist`, `.git`,
  `.planning` — keep `ui/dist` ignored to avoid stale lazy-chunk bugs in
  the served bundle.

### Standalone mode (new)

* New entity `dao.Host` (id, endpoint, auth method, status, engine info,
  TLS / SSH credentials) + DAO methods on both BoltDB and MongoDB backends.
* `docker.HostManager` — per-host `*client.Client` cache; supports
  `unix://`, `tcp://`, `tcp+tls://`, `ssh://` endpoints.
* `MODE=standalone` env / config option (single source of truth via
  `misc.IsStandalone()`).
* `docker.Docker.agent(node)` is now mode-aware: in standalone it routes
  via `HostManager`; in swarm it keeps the swarm-agent socat lookup.
* New per-host endpoints: `ContainerCount`, `ContainerListAll`,
  `ImageCount`, `Network*OnNode` family.
* Swarm-only API endpoints (`/service/*`, `/task/*`, `/config/*`,
  `/secret/*`) return **404** in standalone via the `swarmOnly()` wrapper
  in `api/api.go`. The auto-scaler is disabled. The frontend router guard
  blocks the corresponding route names.

### Container lifecycle (Portainer-style)

* New SDK calls: `ContainerStart`, `ContainerStop`, `ContainerRestart`,
  `ContainerKill`, `ContainerPause`, `ContainerUnpause`, `ContainerRename`,
  `ContainerStats` (one-shot).
* Exposed via API at `/container/{start,stop,restart,kill,pause,unpause,rename,stats}`.
* New permission `container.edit` in `security/perm.go`. New event actions
  `Start/Stop/Kill/Pause/Unpause/Rename` in `biz/event.go`.
* Frontend: new shared component `ui/src/components/ContainerTable.vue`
  with action bar (Start/Stop/Restart/Pause/Unpause/Kill/Details/Delete).
  Delete is disabled while the container is `running` or `paused`.
* Stack filter dropdown + `Stack` column on the standalone Containers
  page (filters via `label=com.docker.compose.project=<project>`).

### Compose stacks for standalone (new)

* New entity `dao.ComposeStack` (id, host id, name, content YAML,
  status, audit) on both BoltDB and MongoDB backends; bucket
  `compose_stack` added to BoltDB init.
* New engine `docker/compose/standalone.go::StandaloneEngine`:
  * `Deploy` — full stop-remove-recreate based on the YAML
  * `Start / Stop / Remove` (optional volumes)
  * `List` — discovers all projects on a host (managed + external) via
    `com.docker.compose.project` label
  * `GetProject(name)` — live project detail
  * `ReconstructCompose(name)` — best-effort YAML reconstruction from
    inspecting running containers
* Supported compose subset: services (image, command, entrypoint, env,
  ports, volumes bind/named/tmpfs, networks, restart, labels, user,
  working_dir, hostname, tty, stdin_open, privileged, read_only,
  cap_add/drop, dns, dns_search), networks, volumes. Not supported:
  build, healthcheck, secrets, configs, deploy, depends_on.
* Label convention identical to the docker-compose CLI
  (`com.docker.compose.project`, `com.docker.compose.service`,
  `com.docker.compose.container-number`) plus
  `com.swirl.compose.managed=true` to mark Swirl-deployed projects.
* Biz `ComposeStackBiz` with `Search / Find / FindDetail / Save /
  Deploy / Import / Start / Stop / Remove` and external variants
  `StartExternal / StopExternal / RemoveExternal` keyed by
  `(hostId, name)`.
* API at `/api/compose-stack/*` including `find-detail` and `import`.
  `start/stop/remove` accept either `{id}` (managed) **or**
  `{hostId, name}` (external) — single endpoint, internal dispatch.
* Frontend pages: `pages/compose_stack/{List, Edit, View}.vue`. Routes
  `std_stack_list`, `std_stack_new`, `std_stack_edit`,
  `std_stack_detail` (managed), `std_stack_external_detail`
  (external, keyed by hostId+name).
* Stack details view (mode-aware): tabs **Overview**, **Containers**
  (uses `ContainerTable` with full action bar), **Compose (YAML)**
  (CodeMirror, editable for external + Import / Import & Redeploy
  workflow). External-stack banner warns about the YAML reconstruction
  approximation.
* **Download** action: persisted YAML download from the Stack list and
  Stack details (managed + reconstructed for external).

### Image / Volume / Network UX

* **Image "Unused" badge** — when no container references the image
  (recomputed server-side via `ContainerListAll` + `ImageID` map; the
  `image.Summary.Containers` field returned by the daemon is unreliable
  / `-1`).
* **Image Force delete** — red action with confirmation; calls
  `client.ImageRemove(Force=true, PruneChildren=true)`.
* **Volume "Unused" badge** — when no container mounts the volume
  (recomputed server-side by scanning `Mounts[].Type==volume` —
  `volume.UsageData.RefCount` is `-1` by default).
* **Bulk delete** for images, volumes and standalone networks
  (`n-data-table` selection column, header "Delete (N)" / "Force
  delete (N)" buttons, per-item error aggregation).
* **Standalone network pages** (`pages/network/StandaloneList.vue`,
  `StandaloneNew.vue`) with host selector and a form that hides
  Swarm-only fields (`overlay` driver, scope, attachable, ingress).
* Backend network operations gain node-aware variants:
  `NetworkListOnNode`, `NetworkCreateOnNode`, `NetworkRemoveOnNode`,
  `NetworkInspectOnNode`. `biz.NetworkBiz` propagates `node` to the
  docker layer; `api/network.go` accepts `node` in query and body.

### Frontend / UX improvements

* **Mode-aware menu** built dynamically from
  `ui/src/router/menu.ts::buildMenuOptions(mode)`.
* **Global host selector** in the header (standalone only): persisted in
  `localStorage`; auto-selects the only host when one is registered;
  refreshes after add/remove (Vuex action `reloadHosts`); shows status
  dot per host.
* **Active menu group always expanded** on navigation while preserving
  manual collapses on other groups.
* **Page-size persistence** (`useDataTable`): chosen rows-per-page
  (10/20/50/100) saved to `localStorage` shared across all tables.
* **Refresh button** in every list page (Containers, Stacks, Networks,
  Images, Volumes).
* **State / Status column moved to the left** in Containers and Stacks
  tables for at-a-glance scanning.
* Stack list **drops the `Host` column** (redundant with the global
  selector) and shows the host name as page-header subtitle.
* `EmptyHostPrompt.vue` shown on per-host pages when "All" is selected.
* **Stack filter** + **Stack column** on the Containers page in standalone.
* **CodeMirror** wrapper supports a new `height` prop and refreshes
  itself after the first render so YAML appears correctly inside lazy
  tab panes. `<root />` template tag bug fixed by switching the App
  root to a render function.

### Bootstrap & infrastructure fixes

* `/api/system/mode` is intentionally `auth:"*"` (public) — required by
  the UI bootstrap before login. Inline-documented to prevent future
  tightening.
* `dao/bolt/bolt.go::New` runs `os.MkdirAll(addr, 0755)` before
  `bolt.Open` so `docker run -e DB_ADDRESS=/data` works without
  pre-creating the volume directory.
* Bootstrap host load happens **post-login** (`store.subscribe` on
  `SetUser`), avoiding 401 + redirect loops at app start.

### Container image (Docker)

* New compose files: `docker-compose.standalone-bolt.yml` (single
  container, BoltDB) and `compose.standalone.yml` (with MongoDB).
* Multi-stage Dockerfile uses `node:22-alpine` and `golang:1.25-alpine`.

### Quality-of-life batch (2026-04)

* **Host detail auto-sync** — after `HostBiz.Create/Update`, Swirl calls
  `docker.Client.Info()` synchronously and persists `ServerVersion / OSType
  / Architecture / NCPU / MemTotal` alongside the status. The Hosts list
  shows the enriched record on the first render after save.
  `dao.HostUpdateStatus` signature extended; BoltDB + MongoDB both updated.
* **Refresh button** added to the Hosts list (header action) for parity
  with every other list page.
* **Home summary — Stacks counter in standalone** — `api/system.go::
  systemSummarize` now aggregates compose projects via
  `compose.NewStandaloneEngine(cli).List(ctx)` per reachable host (both
  single-host and all-hosts paths).
* **Race-free host switch** — `ui/src/utils/data-table.ts::useDataTable`
  tags each `fetchData()` with a monotonically increasing `requestGen`;
  out-of-order responses are dropped so quick host toggles on Images /
  Volumes no longer leave stale rows in the table.
* **Events fixes**:
  * `biz/volume.go::Create` was emitting `EventActionDelete` — corrected.
  * `biz/network.go::Create` emitted only on error — corrected to emit on
    success.
  * Added Create events for `Role`, `Chart`, `Registry` biz methods.
  * `biz/compose_stack.go::Start / StartExternal` now emit `Start` (was
    `Deploy`).
  * New action `EventActionImport` emitted by `ComposeStackBiz.Import` in
    both the save-only and save-and-redeploy branches.
* **Keycloak OIDC integration** —
  * New `misc.Setting.Keycloak` group (`enabled, issuer_url, client_id,
    client_secret, redirect_uri, scopes, username_claim, email_claim,
    groups_claim, auto_create_user, group_role_map, enable_logout`).
  * New user type `keycloak` (in addition to `internal` and `ldap`).
  * New package `security/keycloak.go` (go-oidc/v3 + oauth2): lazy
    provider discovery, 1-hour cache, auth-code URL builder, code
    exchange + ID-token verification, group → role resolver with
    first-match wins.
  * New handlers `api/auth.go`:
    `/auth/keycloak/login` (CSRF state + redirect-to-Keycloak),
    `/auth/keycloak/callback` (exchange → upsert → issue session →
    redirect to `/oauth-complete#…`),
    `/auth/keycloak/logout-url` (RP-initiated logout URL for the
    front-end).
  * New endpoint `/system/auth-providers` returning
    `{ldap: bool, keycloak: bool}` (auth `*`) for the Login page.
  * Frontend: Keycloak panel in `Setting.vue` with Swirl-side / Keycloak-
    side hints per field, `NDynamicInput`-based group-role matrix fed by
    `roleApi.search()`; Login page shows **Login with Keycloak** button
    when enabled; new bridge page `pages/OAuthComplete.vue` reads the
    URL fragment, commits the user to Vuex, then navigates to the
    originally-requested route; the `logout()` handler in
    `layouts/Default.vue` also hits `/auth/keycloak/logout-url` when an
    `id_token` is cached in `localStorage`, then redirects upstream.
  * Dependencies: `github.com/coreos/go-oidc/v3 v3.18.0`,
    `golang.org/x/oauth2 v0.36.0`.

### Network topology view

* New **Topology** tab under `Network`: interactive graph of networks,
  containers, and connectivity for the selected host.
* Visual cues: red highlight for ports published to `0.0.0.0` / `::`
  (publicly exposed); blue border for `internal=true` networks (isolated).
* Layouts: force-directed, circular, radial, sunburst, treemap, sankey,
  hierarchical — picker in the top-right of the canvas.
* New `Host` column on `Events` so audit entries are filterable by host
  in standalone mode.
* Branding refresh: `SwirlEvo` displayed across login, page header and
  system info; bootstrap login fix (loads `/system/mode` before the form
  renders so the right realm options are shown).

### HashiCorp Vault integration

* New `vault/` package: thin HTTP client for Vault KVv2.
  * `vault/client.go` — Token + AppRole auth, `ReadKVv2`, request
    timeout, TLS (`tls_skip_verify`, `ca_cert` PEM), namespace header
    for Enterprise, token cache with TTL, `TestAuth` for the UI test
    button. Token is `strings.TrimSpace`-d to neutralise pasted
    newlines.
  * `vault/backup_provider.go` — implements `biz.BackupKeyProvider` so
    `SWIRL_BACKUP_KEY` can be sourced from a KVv2 entry when the env
    var is empty. 5-minute cache for the resolved passphrase.
  * `vault/wire.go` — DI registration with a closure that always
    resolves the *live* `*misc.Setting` pointer.
* New settings block `Settings.Vault` (`misc/option.go`) covering
  enabled, address, namespace, auth_method, token, approle_*, kv_*,
  backup_key_*, default_storage_mode, TLS, request_timeout.
* New `Settings → Vault` UI panel with **Test connection** action that
  surfaces the actual backend reason (sealed / not initialised / wrong
  token / TLS error). Save now refreshes the in-memory settings
  snapshot in place — closures captured at startup (Vault client,
  backup key provider) see the new values without a restart.
* **VaultSecret catalog**: `dao.VaultSecret` (entity), DAO methods on
  both BoltDB and MongoDB, `biz.VaultSecretBiz` (CRUD + `Preview`
  showing only field names, never values), API at
  `/api/vault-secret/*`, Vue pages at `ui/src/pages/vault-secret/`.
  Permissions: `vault_secret.{view,edit,delete}`.
* **Per-stack secret bindings** (standalone compose stacks):
  * `dao.ComposeStackSecretBinding` entity + DAO methods (Mongo + Bolt).
  * `biz.ComposeStackSecretBiz` with materializer hook implementing
    `compose.DeployHook` (`BeforeDeploy` / `ApplyToService` /
    `AfterCreate` / `AfterRemove`); resolves the Vault value once per
    deploy, computes `sha256` and stores it as `DeployedHash`.
  * Three storage modes for files: **`tmpfs`** (in-memory tmpfs over
    parent dir + `CopyToContainer` between Create and Start;
    multiple bindings sharing the same parent collapse to one
    mount), **`volume`** (project-scoped named volume populated by a
    short-lived `busybox` helper container), **`init`** (same as
    `volume` but the helper persists for audit). Plus **`env`** for
    environment-variable injection.
  * Cleanup is label-driven (`com.swirl.compose.secret-stack`,
    `com.swirl.compose.secret-binding`) via a separate `cleanupHook`
    so stack removal works even when Vault is unreachable.
  * UI: bindings panel inside the standalone stack editor with add /
    edit / delete, validation matching the biz layer, Vault secret
    picker populated from `/vault-secret/list`.
  * API at `/api/compose-stack-secret/{list,find,save,delete,drift}`,
    permissions inherited from `stack.{view,edit}`.
* **Drift check**: `CheckDrift(stackID)` per binding compares the
  current Vault value's `sha256` against the stored `DeployedHash`.
  States: `ok`, `drifted`, `missing`, `error`, `unknown`. Read-only,
  best-effort, per-binding tolerant — surfaces orange/red badges next
  to the deploy timestamp in the bindings table.
* `BackupDocument` now includes `vaultSecrets` and
  `composeStackSecretBindings` arrays (references only, never values).
  Restore order respects dependencies: vault secrets before bindings.

### Internal backup management

* New backup subsystem (`biz/backup.go`, `biz/backup_crypto.go`,
  `backup/backup.go`, `api/backup.go`):
  * AES-256-GCM at-rest archives (`.swb` magic `SWBR`); 12-byte random
    nonce; 32-byte key derived from `SWIRL_BACKUP_KEY` via
    `scryptKDF(N=32768, r=8, p=1)` with fixed salt
    `swirl-backup-at-rest`.
  * Portable export format (`.enc` magic `SWBP`) with random per-file
    salt — share archives across instances using only a passphrase
    chosen at download time.
  * Atomic file writes (`writeFileAtomic` = temp + `rename(2)`),
    `0600` file mode, `0750` directory mode, configurable directory
    via `SWIRL_BACKUP_DIR` (default `/data/swirl/backups`).
  * Manual + scheduled backups (`daily`, `weekly`, `monthly`) with
    retention; hourly scheduler tick; one schedule per type.
  * Component-selective restore (settings, roles, users, registries,
    Swarm stacks, compose stacks, hosts, charts, vault secret refs,
    binding refs, events). Events are opt-in.
  * Permissions: `backup.{view,edit,delete,restore,download}`.
* **Key compatibility check + recovery** (new):
  * Backup records now carry a 16-byte HMAC-SHA-256 fingerprint of
    the master key they were encrypted with (label
    `swirl-backup-key-fp/v1`). Stored in `dao.Backup.KeyFingerprint`
    + `VerifiedAt` (both with `omitempty` for backward compatibility).
  * Non-blocking startup goroutine compares stored fingerprints
    against the current key's fingerprint and logs one summary line
    (`backup key check: N/M compatible …`); per-failure detail
    available via the API/UI. Legacy backups (no stored fingerprint)
    appear as `unverified` until the operator clicks **Verify**.
  * UI: per-row badge (`compatible` / `incompatible` / `unverified` /
    `missing` / `unknown`) + page-level error banner counting
    incompatibles + dialog accepting the **old** `SWIRL_BACKUP_KEY`,
    decrypting with it via the new `decryptAtRestWithKey` helper, and
    re-encrypting in place with the current master key. Atomic
    rewrite + `BackupUpdate` DAO method (split from insert-only
    `BackupCreate`); per-id mutex serialises Recover vs Delete.
  * New API endpoints: `/backup/key-status`, `/backup/verify`,
    `/backup/recover`. New permission **`backup.recover`**
    (bit `1 << 13`), distinct from `restore` and `edit`.

### i18n

* Italian locale added (`ui/src/locales/it.ts`), wired in
  `locales/index.ts`, `Profile.vue` (radio button), and `App.vue`
  (Naive UI `itIT` + `dateItIT`). UI now ships **English / Italiano /
  中文**.
* Missing `buttons.prev` / `buttons.next` keys added to all three
  locales (the backup restore/upload wizard buttons used to render the
  raw key path).

### Sensitive field masking in Settings UI

* Vault token, Vault secret id, and Keycloak client secret are never
  round-tripped through the UI in cleartext any more. On GET the
  backend returns a visible placeholder `••••••••`; on Save, the
  placeholder (or an empty string) means "preserve the existing
  value", while a different value overwrites it.
* Implementation: `biz.SettingSecretMask` constant, `sanitizeForResponse`
  and `preserveSecretsFromExisting` helpers in `biz/setting.go` —
  `Find`/`Load` sanitize on egress, `Save` preserves on ingress.
  `refreshInMemory` uses a dedicated `loadRaw` path so the live
  `*misc.Setting` snapshot keeps real values.
* `SETTING_SECRET_MASK` export in `ui/src/api/setting.ts` for future
  UI affordances.

### Vault client: WriteKVv2, DeleteKVv2, ReadMetadataKVv2

* New methods on `vault.Client` implementing the missing KVv2 surface:
  POST to `<mount>/data/<path>` (write), DELETE on
  `<mount>/metadata/<path>` (full delete incl. version history), GET on
  `<mount>/metadata/<path>` (version metadata).
* `ReadMetadataSummary(ctx, path) → (current, total, exists, err)` —
  primitive-typed projection consumed by the biz layer (avoids
  exporting `KVv2Metadata` through the `biz.vaultReader` interface).
* HTTP/2 default restored: TLS 1.3, ALPN negotiation, keep-alives,
  Go default headers. The JA3/WAF/TLS mitigations introduced during
  the Traefik debug session are gone — the root cause was a Traefik
  `internal-ips` ACL, not the client. Kept: `strings.TrimSpace` on
  the token, and `resp.Proto`/`Server`/`Via` in 4xx/5xx error
  messages (useful for future reverse-proxy debugging).

### Vault secret value writes (from UI)

* `VaultSecretBiz.WriteValue(id, data, replace, user)` writes a new
  KVv2 version directly from Swirl. `replace=false` merges fields
  with the current version; `replace=true` produces a version with
  only the supplied fields. Values never touch disk — the audit
  event records only field NAMES.
* New endpoint `POST /api/vault-secret/write` gated by
  `vault_secret.edit`. Requires the Vault token to have
  `create`+`update` on the KVv2 data path.
* UI: new "Set value" panel in the VaultSecret editor with
  append/replace radio, password-typed dynamic inputs, confirmation
  modal.

### VaultSecret version badge + UX refresh (Vault Secrets pages)

* `VaultSecretBiz.GetStatuses(ctx)` fetches per-catalog-entry metadata
  from Vault in parallel (concurrency capped at 8). New endpoint
  `GET /api/vault-secret/statuses` returns `{id → {exists, current,
  total, error}}`.
* New reusable component `ui/src/components/VersionBadge.vue` used
  by both the list and editor pages.
* List page redesign: filter bar with free-text + multi-label
  filters, bulk-delete toolbar, empty state CTA, version badge
  column, row highlighting for missing entries. Tooltips on the
  badge explain OK / missing / error states.
* Editor redesign: collapsible "Catalog entry" panel, read-only
  "Vault status" panel with full resolved path + current version
  + field list, and a dedicated "Set value" panel driving the new
  write endpoint.
* New i18n keys under `vault_secret.*` in en/it/zh.

### Backup storage toggle (filesystem | Vault KVv2)

* New settings group `misc.Setting.Backup` with `storage_mode`
  (`fs` default | `vault`) and `vault_prefix` (default `backups`).
  Frontend panel in Settings → Backup storage.
* Storage abstraction in `biz/backup.go`:
  - schema-prefixed `rec.Path` (`file://…` | `vault:…`); legacy rows
    with no prefix are treated as filesystem for backward compat
  - helpers `writeArchive`, `readArchive`, `rewriteArchive`,
    `deleteArchiveByPath`, `archiveMissing` dispatch on the schema
  - `Create` / `Delete` / `Open` / `Restore` / `Verify` / `VerifyAll`
    / `Recover` all route through the helpers
* Vault mode base64-encodes the already-AES-encrypted archive and
  stores it as `{archive, created_at}` under
  `<kv_mount>/data/<kv_prefix><vault_prefix>/<id>`. KVv2 default 1 MiB
  entry limit applies; ceiling raised by operator via Vault settings.
* Policy to add when `storage_mode=vault`:
  ```hcl
  path "<mount>/data/<prefix>/backups/*"     { capabilities = ["create","update","read","delete"] }
  path "<mount>/metadata/<prefix>/backups/*" { capabilities = ["read","list","delete"] }
  ```

### User.Type Keycloak editable

* Edit user form now shows the Type radio always (not only when
  editing) with three options: Internal / LDAP / Keycloak. Password
  fields render only when `type === 'internal' && !id`.
* `biz.userBiz.Update` clears `Password` + `Salt` when the type is
  switched away from internal (via `UserUpdatePassword`). No more
  dead hashes left in the DB after an Internal→Keycloak migration.

### Registry v2 browse + self-signed TLS opt-in

* New `SkipTLSVerify bool` on `dao.Registry` (with `omitempty` JSON +
  BSON tags, default `false`). Persisted by both Mongo (via
  `$set.skip_tls_verify`) and Bolt (via struct marshal). UI checkbox
  in Registry Edit.
* New file `docker/registry.go` — minimal HTTP client for Docker
  Registry v2. Per-registry `http.Client` cache keyed by
  `registry.ID` with a config hash; rebuilds when URL or
  `SkipTLSVerify` flip. Basic auth from `dao.Registry`.
  `CatalogList(pageSize, last)` with RFC-5988 Link header parsing
  for pagination; `TagsList(repo)` straightforward.
* New biz methods `Browse(id, pageSize, last)` and `Tags(id, repo)`
  on `RegistryBiz`. New endpoints `GET /api/registry/browse` and
  `GET /api/registry/tags`, both gated by `registry.view`.
* UI: Registry detail page rebuilt as tabs — "Detail" (original
  read-only fields) + "Repositories" with filter, paginated load,
  per-row "Show tags" drawer.

### Image tag + push

* `docker.Docker.ImageTag(node, source, target)` and
  `docker.Docker.ImagePush(node, ref, authBase64)` wrappers on the
  Docker SDK. Push drains the progress stream; large-image pushes
  hit a 10-minute API timeout.
* `biz.ImageBiz.Tag(node, source, target, user)` and `Push(node,
  ref, registryID, user)`. Push resolves auth via
  `RegistryBiz.GetAuth(url)` so the encoded AuthConfig never leaks
  beyond the biz layer.
* New permission `image.push` (bit `1 << 14`). `Perms["image"]`
  extended to `{"view", "edit", "delete", "push"}`.
* New endpoints `POST /api/image/tag` (auth `image.edit`) and
  `POST /api/image/push` (auth `image.push`).
* UI: two new row actions in `image/List.vue` — "Add tag" modal +
  "Push" modal with a registry picker populated from
  `/registry/search` and the current image's existing tags.

### Container listing & bulk actions

* Status filter (All / Running / Exited / Created / Paused) in the
  container list. Backend already supported the `status` query param;
  frontend now surfaces it as a dropdown.
* Bulk actions: checkbox selection column in `ContainerTable.vue`
  (`selectable` prop) + toolbar buttons Start(N) / Stop(N) /
  Restart(N) / Delete(N) with confirmation dialog for destructive
  operations. Aggregated error reporting.

### Deploy error persistence

* `dao.ComposeStack.ErrorMessage` field (persisted, `omitempty`).
  New DAO method `ComposeStackUpdateError`.
* `biz.Deploy`: saves `err.Error()` on failure, clears on success.
* View page Overview tab: `<n-alert>` shown when `errorMessage` is
  non-empty — persistent, survives page reload, disappears after
  successful redeploy.

### Binding wizard (multi-field, service picker, env name mapping)

* New multi-step wizard modal in the stack editor:
  Step 1 — select VaultSecret; Step 2 — pick fields from Preview
  (checkboxes); Step 3 — configure each selected field (service
  picker from compose YAML, env var name editable, target type).
* `parseServiceNames()` extracts service names from compose YAML
  via indent-aware regex (no external YAML parser dependency).
* `dao.ComposeStackSecretBinding.Field` — per-binding field override
  so the materializer knows which KVv2 field to extract (catalog
  entry's Field is no longer forced to `"value"` default).
* Materializer auto-expand: if `bind.Field` is empty and the KVv2
  entry has multiple fields, each is injected as a separate env var
  (no more JSON blobs as env values).

### Environment variables (.env) for stacks

* `dao.ComposeStack.EnvFile` field — key=value lines, persisted.
* `docker/compose/standalone.go::Deploy`: `DeployOptions.EnvVars`
  injected via `os.Setenv` before compose Parse so `${VAR}`
  references in the YAML are expanded.
* UI: textarea panel "Environment variables (.env)" in the stack
  editor between YAML and secret bindings. Read-only display in the
  View page under the compose tab.
* Stack download: ZIP file with `docker-compose.yml` + `.env` +
  `.secret` (variable names only, never values). Shared `buildZip`
  utility in `ui/src/utils/zip.ts`.

### Registry v2 browse: token-based auth fix

* Full Docker Registry v2 Token Authentication flow: 401 + Bearer
  challenge → fetch token from realm → retry with Bearer header.
  Fixes "Request failed with status code 500" on Docker Hub, Harbor,
  GitLab, and other hosted registries.
* Registry status badge: `GET /api/registry/ping` endpoint + status
  column in the registry list (OK/Error with tooltip showing the
  exact error message).

### Keycloak OIDC login (complete rewrite of auth flow)

* **Routing fix**: tag paths in `AuthHandler` were absolute (e.g.
  `/auth/keycloak/login`) but the framework prepends the Handle
  prefix `/auth` from the container name `api.auth` — resulting in
  double `/auth/auth/keycloak/login`. Changed to relative paths
  (`/keycloak/login`, `/keycloak/callback`, `/keycloak/logout-url`).
  The login was broken since the original implementation.
* **Nil pointer fixes**: `biz.newOperator(nil)` and
  `eventBiz.create(nil user)` now handle nil `web.User` — covers
  all system-initiated operations (Keycloak auto-create/update,
  scheduler, etc.) that don't have an authenticated web context.
* **OIDC HTTP client**: `oauth2`/`oidc` libraries now use a custom
  `http.Client` with `DisableKeepAlives: true` (injected via
  `oauth2.HTTPClient` context key) to avoid "Misdirected Request"
  from reverse proxies.
* **Group slash mismatch**: Keycloak sends `/appFoo` (full path);
  code stripped the `/` prefix but the mapping used it.
  `resolveRoles` now tries match with and without `/`.
* **Role mapping by name**: `resolveRoles` supports both role IDs
  (old format) and role names (new format that survives
  backup/restore). If the value isn't a valid role ID, it's looked
  up by name.
* **Import from OpenID Configuration**: paste the URL or JSON from
  Keycloak's "OpenID Endpoint Configuration" page to auto-populate
  `issuer_url`, `redirect_uri`, and defaults for scopes/claims.
* **Diagnostic test**: `GET /api/setting/keycloak-test` runs 4
  checks (config completeness, OIDC discovery, redirect_uri,
  auth URL generation) and returns structured OK/FAIL per check.
* **OAuth race condition**: 100ms delay in `OAuthComplete.vue`
  before navigating to the final redirect, so the Vuex store
  propagates the token before the target page fires API calls.

### Permissions UI completeness

* `ui/src/utils/perm.ts` was missing several resources and actions
  that were added to `security/perm.go` over time: `host` (view /
  edit / delete), `backup` (view / edit / delete / restore /
  download / recover), `image.edit`, `image.push`, `container.edit`.
  Without these entries, the Role editor couldn't display or set
  the corresponding checkboxes → any role assigned to a Keycloak
  user was missing host/backup/image permissions.
* Added `perms.recover` and `perms.push` i18n labels in en/it/zh.

### VaultSecret catalog: Field handling

* Removed the forced `Field = "value"` default from the VaultSecret
  normalizer — empty Field now means "auto-select" (single-field
  entries use the sole value; multi-field returns JSON).
* `extractSecretValue` fallback: if the requested field isn't found
  but the entry has exactly one field, use it. Error message now
  lists available fields to help the operator fix the mismatch.
* Field column added to the binding table + edit modal in the stack
  editor so the original Vault field name is always visible.
* Field input removed from the VaultSecret catalog editor (the
  catalog now only stores path, not field — field selection belongs
  to the binding).

### Compose parser: `depends_on` long form

* New type `composetypes.DependsOnList []string` replaces the previous
  `[]string` alias on `ServiceConfig.DependsOn`. Registered in
  `createTransformHook` via the new `transformDependsOnList`.
* The parser now accepts **both** compose v3 forms:
  ```yaml
  depends_on:
    - serviceA
    - serviceB
  ```
  and
  ```yaml
  depends_on:
    serviceA:
      condition: service_healthy
    serviceB:
      condition: service_started
      restart: true
      required: true
  ```
  Previously only the short form parsed successfully; the long form
  triggered a type-mismatch error and aborted the deploy.
* **Semantics unchanged**: only the service names are retained. The
  `condition`, `restart` and `required` sub-keys are parsed and
  **silently discarded** — the standalone engine does not enforce
  readiness ordering between services. Values from the compose v2
  ecosystem (`service_healthy`, `service_started`,
  `service_completed_successfully`) are accepted syntactically but
  have no runtime effect.
* Map keys are sorted for a deterministic `DependsOn` slice.
* Covered by 4 new tests in `docker/compose/parse_test.go`, including
  `TestParseDependsOnUserRegression`.

### Documentation refresh

* `docs/vault.md`, `docs/backup.md`: updated for KVv2 write, backup
  Vault storage, registry self-signed TLS, VaultSecret field
  changes, env file support, and revised policy snippets.
* `README.md` Features list extended with container bulk actions,
  deploy error persistence, env file support, Keycloak login,
  registry browse with token auth.
* `README.md` "Compose stacks in standalone mode" section revised
  to reflect the new `depends_on` long-form support (parsed but
  ordering not enforced); `depends_on` removed from the "Not
  supported" bullet.
* `.claude/agents/swirl-expert.md`: comprehensive update covering
  all new subsystems, patterns, and warnings.

### Compose standalone: strict validation of `build:` (Opzione A1)

* **Symptom**: deploying a compose file that used `build.context`
  (with or without `image:`) produced a confusing
  `"service X: Error response from daemon: no command specified"`
  error — a generic daemon message that masked the real problem.
* **Cause**: the compose parser accepted `build:` (it is in the
  schema), but the standalone engine never calls `ImageBuild`. The
  service reached `ContainerCreate` with an empty image reference,
  and the daemon returned its generic "no command specified" error.
* **Fix**: new pure function `validateServices(cfg *composetypes.Config) error`
  in `docker/compose/standalone.go`, invoked by `Deploy` immediately
  after `Parse` and before any side effect (no pull, no network/volume
  ensure, no hook). The function fails fast with two rules:
  1. any service with `svc.Build.Context != ""` is rejected with
     `service <name>: 'build:' is not supported in standalone mode;
     pre-build the image and reference it with 'image:' only` —
     regardless of whether `image:` is also set;
  2. any service with neither `image:` nor `build:` is rejected with
     `service <name>: neither 'image:' nor 'build:' is set; an image
     reference is required`.
* **Retro-compatibility**: compose files that rely solely on `image:`
  continue to work unchanged. `ImageBuild` is NOT implemented (that
  would have been the rejected Opzione B).
* **Tests**: new file `docker/compose/standalone_test.go` covers
  image-only (pass), build-only (fail), build+image (fail), neither
  (fail), fail-fast on first offender, nil config, and empty
  services. All assertions work without a live Docker daemon —
  `validateServices` is pure.

### Self-deploy (v1, standalone only)

Swirl can now redeploy itself from the UI. Click **Settings → Self-deploy
→ Deploy now**; a short-lived sidekick container (`swirl-deploy-agent-*`)
stops the running Swirl, pulls the new image, deploys the new stack,
verifies `/api/system/mode` answers, and — on failure — either rolls
back automatically or exposes a tiny allow-listed recovery UI.

* **Scope v1**: standalone mode only. Swarm mode is blocked at the biz
  level (`ErrSelfDeployBlocked`). The UI hides the panel. A future
  version will tackle Swarm (rolling service update with rollback).
* **Safety pivot**: the old container is **renamed** to `swirl-previous`,
  never removed, until the new Swirl is healthy. Auto-rollback
  (default on) renames it back if anything fails. Worst case: you
  land back on the version you started from, plus one audit entry.
* **Sidekick model**: spawned on demand via the Docker socket; no
  persistent agent. Mounts the state directory (`/data/self-deploy/`)
  + socket + runs with `NetworkMode=host` so it can bind the recovery
  port and reach the new Swirl's expose port regardless of compose
  network state. `AutoRemove=false` so operators can `docker logs` it
  after the fact.
* **Recovery UI**: embedded HTTP server exposing `/`, `/logs`,
  `/retry`, `/rollback`. IP allow-list (CIDR) + one-time CSRF token
  per session, no password. Default bind `127.0.0.1:8002`. Setting
  `RecoveryAllow=0.0.0.0/0` is accepted but produces a WARN-level log
  entry on every deploy.
* **Template + placeholders**: the compose YAML is a Go `text/template`
  with typed placeholders (`ImageTag`, `ExposePort`, `RecoveryPort`,
  `RecoveryAllow`, `TraefikLabels`, `VolumeData`, `NetworkName`,
  `ContainerName`, `ExtraEnv`). Defaults live in
  `misc/self_deploy_defaults.go`. `Preview` renders + parses through
  `compose.Parse` so bad templates fail before they reach disk.
* **Env requirement**: the primary Swirl must be started with
  `SWIRL_CONTAINER_ID=${HOSTNAME}` (or equivalent) so it can identify
  the container the sidekick must swap out. See the shipped example
  `compose.self-stack.yml.example`.
* **API endpoints** (mounted at `/api/self-deploy`):
  - `GET /load-config` — `self_deploy.view`
  - `POST /save-config` — `self_deploy.edit`
  - `POST /preview` — `self_deploy.view`
  - `POST /deploy` — `self_deploy.execute`, returns HTTP 202 Accepted
  - `GET /status` — `self_deploy.view`, idempotent audit-event emitter
* **Permissions** (new in `security/perm.go`): resource
  `self_deploy` with actions `{view, edit, execute}`. Mirrored in
  `ui/src/utils/perm.ts` so the Role editor can grant them.
* **Audit events**: new `EventTypeSelfDeploy` with actions
  `Start` (emitted by TriggerDeploy on successful sidekick spawn) +
  `Success` / `Failure` (emitted by the main Swirl's Status handler
  when it observes a terminal phase in `state.json` — the sidekick has
  no DB access). Idempotency via the `EventPublished` flag inside
  `state.json` so repeated polls never duplicate the audit entry.
* **Invariants** (enforced before spawn, double-checked by sidekick):
  `PrimaryContainer` is non-empty AND exists on the daemon;
  `TargetImageTag` is non-empty; `ComposeYAML` passes `compose.Parse`
  (which runs the strict standalone rules — no `build:`, `image:`
  required per service); `RecoveryPort != ExposePort`;
  external networks/volumes referenced by the compose file exist on
  the host; no service `container_name` collides with the sidekick
  naming pattern `swirl-deploy-agent-*`. `RecoveryAllow=0.0.0.0/0`
  is a warning, not an error.
* **Operator docs**: [`docs/self-deploy.md`](docs/self-deploy.md) —
  prerequisites, first-deploy bootstrap, subsequent-deploy workflow,
  recovery, rollback, troubleshooting, security considerations,
  limitations.
* **Seed example**: [`compose.self-stack.yml.example`](compose.self-stack.yml.example) —
  operator-facing starting point for the first deploy. Valid YAML out
  of the box; comments explain every field an operator will change.
* **i18n**: new keys `events.type.self_deploy`,
  `events.action.self_deploy_start|success|failure` in en / it / zh.

---

## v1.0.0 (2021-12-15)

> As this version contains some incompatible modifications, it is recommended to redeploy instead of upgrading directly.
 
* feat: Refactor UI with vue3.
* feat: Add support to agent. Swirl can connect to any container even in swarm mode now.
* feat: Support token authentication.
* feat: Switch to official MongoDB driver.
* feat: Allow set chart margins.
* fix: Some args are incorrect when generating service command line.
* break: Optimize role permissions.
* break: Adjust system settings.
