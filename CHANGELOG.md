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
