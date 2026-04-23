# Stack Addons

The Stack editor in Swirl is split into two worlds:

1. **Compose** — the raw `docker-compose.yml` editor (CodeMirror),
   the source of truth for every service.
2. **Addon wizard tabs** — structured editors that own a specific
   label namespace (Traefik, Sablier, Watchtower, Backup) or a
   specific scalar-field area (Resources, Registry Cache, Secrets).
   Save-time injection rewrites the YAML; on load a reverse parser
   reconstructs the form state from the persisted labels.

The intent: operators configure reverse-proxy routes, sleep-and-wake
policies, update schedules, backup jobs, resource limits, and
registry-cache opt-out from clear tabs, without hand-editing the
YAML for every common case.

---

## Principles

### Label namespace ownership

Each addon owns a set of **label key prefixes**
(`biz/compose_stack_addons_yaml.go::addonPrefixes`):

| Addon | Prefixes owned |
|---|---|
| Traefik | `traefik.` |
| Sablier | `sablier.` (native container-label form) |
| Watchtower | `com.centurylinklabs.watchtower.` |
| Backup | `backup.` |

When the wizard saves, **for every service listed in the wizard's
config** (`cfg.<Addon>[svc]`), Swirl:

1. Strips every label under the addon's prefixes from that service
   (in **both** standalone `services.<svc>.labels` and swarm
   `services.<svc>.deploy.labels` — the cross-location purge
   prevents orphans when a stack later migrates between modes).
2. Writes the labels computed from the form into the *mode-correct*
   target location (standalone → `labels`; swarm → `deploy.labels`).

Services **not** listed in `cfg.<Addon>` are left untouched. That
means a one-off hand-crafted label survives as long as the operator
never opens the wizard for that service.

**Cross-addon safety note**: the Sablier-via-Traefik plugin form
(`traefik.http.middlewares.<name>.plugin.sablier.*`) lives under
the `traefik.` namespace on purpose. Purging that would nuke every
Traefik middleware of the service. Sablier-on-Traefik entries are
configured under the **Traefik** tab as passthrough rows; the
Sablier tab's prefix is limited to native `sablier.*` container
labels only.

### Reverse parsing (no markers)

The load-time reverse parser (`extractAddonConfig`) walks each
service's labels and assigns any entry whose key starts with a
recognised addon prefix to that addon's config. There is **no
marker comment** distinguishing wizard-authored entries from
hand-authored ones. This is intentional: it matches the
save-time semantics, where the wizard owns the whole namespace on
a touched service. Hand-authored entries become wizard-manageable
the moment the operator opens the wizard for that service — and
the wizard preserves them in its `Labels` map.

### Tab visibility

Tabs are gated by the host's `AddonConfigExtract` (see [Host-level
addon configuration](#host-level-addon-configuration) below):

| Tab | Visible when |
|---|---|
| Compose | always |
| Secrets | standalone mode only |
| Traefik | `host.AddonConfigExtract.traefik.enabled === true` |
| Sablier | `host.AddonConfigExtract.sablier.enabled === true` |
| Watchtower | `host.AddonConfigExtract.watchtower.enabled === true` |
| Backup | `host.AddonConfigExtract.backup.enabled === true` |
| Resources | always |
| Registry Cache | always (but disabled unless feature is enabled globally) |

The host opts in to each addon from **Hosts → Edit → Addons**; the
stack editor reflects that choice without the operator having to
configure the stack to even reach the addon.

### List chips (`ActiveAddons`)

`detectActiveAddons(*ComposeStack)` returns an ordered list of
chip tags which the stack list renders as at-a-glance badges:

- `traefik`, `sablier`, `watchtower`, `backup`, `resources` — present
  when the reverse parser found at least one service configured for
  that addon.
- `registry-cache` — present when `!stack.DisableRegistryCache && the
  YAML contains the `swirl-managed-registry-cache:` marker`.

Order matches the wizard tab order.

---

## Host-level addon configuration

Host edit page (`ui/src/pages/host/Edit.vue`) hosts an **Addons**
section that persists the operator's opinions about what's
installed on the daemon. The full blob is stored on
`Host.AddonConfigExtract` (JSON text on the DAO entity) and round-
tripped via:

- `GET /api/host/addon-extract-get?hostId=<id>` (auth `host.view`)
- `POST /api/host/addon-extract-save` (auth `host.edit`) — takes a
  body with the sub-tree to replace (Traefik / Sablier / Watchtower
  / Backup / RegistryCache); `null` clears a sub-tree.
- `POST /api/host/addon-extract-clear` (auth `host.edit`) — takes
  an optional `addon` field to clear a single sub-tree, or the
  whole blob when omitted.

### `TraefikExtract`

Curated list of what's available on this host's Traefik: discovered
via Docker SDK (`inspect` of the running container — args + env) +
uploaded `traefik.yml` (parsed server-side, **not persisted**:
ACME keys must not be kept in Swirl's DB).

| Field | Purpose |
|---|---|
| `enabled` | Master gate — hides the stack-editor Traefik tab when false |
| `entryPoints`, `certResolvers`, `middlewares`, `networks` | Union of docker-inspect + file extract; dedup preserved |
| `stackId`, `containerName` | Pointer to the Swirl-managed stack (or external container) running Traefik on this host — informational badge in the wizard |
| `defaults` | Pre-fill values (default entrypoint, default certresolver, …) the wizard seeds new rows with |
| `overrides` | Free-form key/value hints the operator uses to document host-specific conventions — shown read-only as annotations in the wizard |

**Provenance** — for each discovered list item, the backend records
whether it came from `docker-inspect` of the running container
(`"docker"`), the uploaded file (`"file"`), or a host annotation
(`"host"`). The UI shows the provenance as a small tag next to each
entry so operators know what would change if they rebuilt the
running Traefik container vs updated the uploaded config.

### `GenericAddonExtract` (Sablier / Watchtower / Backup)

Smaller shape — these addons don't ship a canonical static config
file Swirl would parse:

| Field | Purpose |
|---|---|
| `enabled` | Master gate (tab visibility) |
| `stackId`, `containerName` | Informational pointer to the managed stack |
| `defaults` | Pre-fill values (default Watchtower poll, Backup schedule, Sablier session duration) |
| `overrides` | Free-form read-only annotations |

### `RegistryCacheExtract`

See [docs/registry-cache.md](registry-cache.md#host-bootstrap-per-host-opt-in).
The host-level opt-in is persisted here; the bootstrap script and
daemon snippet are **re-computed on every read** against the live
global setting — the extract stores only the toggle state + the
operator's "applied" attestations.

---

## Wizard tabs

### Traefik tab

Location: stack editor → Traefik (gated).
Component: `ui/src/components/stack-addons/AddonTabTraefik.vue`.

The form presents a structured editor (`StructuredLabelsEditor.vue`)
per service selected from a dropdown (services discovered from the
current compose YAML). Rows are composed of:

- **section** — the Traefik group (`routers`, `services`,
  `middlewares`, `tls`, …). Autocomplete from
  `utils/stack-addon-schemas.ts::makeTraefikSchema`.
- **name** — the router/service/middleware identifier.
- **key** — the terminal property (`rule`, `entrypoints`,
  `loadbalancer.server.port`, …). Autocomplete filtered by section.
- **value** — the scalar value.

Each row renders as exactly one label
(`traefik.<section>.<name>.<key> = <value>`). The master switch
`cfg.Enabled` emits `traefik.enable=true`. A **Raw passthrough
block** below the structured rows lets the operator add or view
labels that don't fit the structured schema (including Sablier-via-
Traefik plugin entries — these live under the Traefik namespace by
design).

A **Label preview** (`LabelPreview.vue`) shows the final label
map — a fidelity check before Save.

### Sablier / Watchtower / Backup tabs

Same flat shape (`Enabled` + `Labels`). The per-addon
`StructuredLabelsEditor` uses its own schema (preset sections + key
autocomplete) from `utils/stack-addon-schemas.ts`. Save writes the
labels verbatim under the addon's prefix on each listed service.

### Resources tab

Location: stack editor → Resources.
Component: `ui/src/components/stack-addons/AddonTabResources.vue`.

**Not labels** — scalar fields. Four values per service:

| Form field | Standalone (`applyResources`) | Swarm (`applyDeployResources`) |
|---|---|---|
| `cpusLimit` | `services.<svc>.cpus` | `services.<svc>.deploy.resources.limits.cpus` |
| `memoryLimit` | `services.<svc>.mem_limit` | `services.<svc>.deploy.resources.limits.memory` |
| `cpusReservation` | `services.<svc>.cpus_reservation` | `services.<svc>.deploy.resources.reservations.cpus` |
| `memoryReservation` | `services.<svc>.mem_reservation` | `services.<svc>.deploy.resources.reservations.memory` |

The backend chooses the target location from the stack's mode
(standalone vs swarm). Empty fields are cleared from the YAML on
save (not written as empty strings).

### Registry Cache tab

Location: stack editor → Registry Cache.
Component: `ui/src/components/stack-addons/AddonTabRegistryCache.vue`.

Surfaces the stack's interaction with the global registry cache:

- **Disable Registry Cache** checkbox →
  `ComposeStack.DisableRegistryCache`. Opts out of deploy-time
  image rewriting. Persisted on Save.
- **Preview** button → `POST /api/compose-stack/registry-cache-preview`.
  Dry-runs the rewriter and renders the decision table (rewritten
  refs + reasons: `digest-preserved`, `invalid-ref`, `already-mirror`,
  `no-match`).
- **Warm** button (permission `stack.deploy`) → `POST
  /api/compose-stack/registry-cache-warm`. Pre-pulls the rewritten
  refs through the Swirl daemon, so the mirror is warm before the
  target host pulls.

Disabled (read-only warning) when the global
`Setting.RegistryCache.Enabled` is false.

See [docs/registry-cache.md](registry-cache.md) for the rewriting
rules, federation delegation and CA rotation.

### Secrets tab (standalone only)

Reuses the VaultSecret feature's per-stack bindings (mount modes:
`tmpfs` / `volume` / `init` / `env`). Hidden in swarm mode because
Swarm has native Docker Secrets (the `secrets:` compose key). See
[docs/vault.md](vault.md) for the underlying materializer model.

---

## Save / Deploy pipeline

`api/compose_stack.go` exposes two Save flavours that accept the
addon wizard state:

- `POST /save` — persists only.
- `POST /deploy` — persists + applies to the host.

Both accept a DTO that wraps:

```json
{
  "stack": {"id":"…", "content":"…yaml…", "envFile":"…"},
  "addons": {
    "traefik":    { "<svc>": { "enabled":true, "labels":{…} }, … },
    "sablier":    { "<svc>": { "enabled":true, "labels":{…} }, … },
    "watchtower": { "<svc>": { "enabled":true, "labels":{…} }, … },
    "backup":     { "<svc>": { "enabled":true, "labels":{…} }, … },
    "resources":  { "<svc>": { "cpusLimit":"…", … }, … }
  }
}
```

Server-side sequence (`composeStackBiz.SaveWithAddons`):

1. `snapshotIfChanged(prev, reason="save")` — see
   [docs/stack-versioning.md](stack-versioning.md).
2. `injectAddonLabels(content, addons, mode)` — cross-location
   purge + rewrite + scalar-field Resources.
3. Write the result into `ComposeStack.Content`.
4. Emit `stack.update` (or `stack.deploy` for the deploy path).

`POST /parse-addons` (auth `stack.view`) reverse-parses a raw
compose YAML into an `AddonsConfig` — used by the editor on load
and by the preview flow.

### `LastWarnings` on the stack

`ComposeStack.LastWarnings []string` — non-fatal observations from
the most recent deploy. Examples:

- Swarm-only fields present in a stack deployed to standalone
  (silently ignored by the standalone engine).
- Standalone-only fields in a swarm deploy.
- Deprecated syntax upgraded silently.

Cleared on a clean successful deploy; overwritten on every redeploy.
Shown as an amber banner on the Overview tab. This is **not** an
error signal — the deploy succeeded — but it flags divergence
between what the operator authored and what actually ran.

---

## Stop semantics

`composeStackBiz.Stop` implements `docker compose down` semantics:
**every container in the project is removed, but named volumes are
preserved**. The stack record stays with `Status=inactive`; the
next `Deploy` recreates containers from the same YAML with the
same data.

Rationale: the old per-container `docker stop` left behind containers
that no longer matched the authored state (network changes, label
changes, image updates wouldn't take effect on the next Start). The
new semantics make Start idempotent with Deploy.

Consequence for the audit log: `Stop` emits `stack.stop` (unchanged);
`Start` may internally call Deploy when no containers remain (typical
post-Stop state), emitting `stack.deploy` at that point. The Events
page correctly distinguishes both.

---

## API reference (addon-specific)

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/api/compose-stack/parse-addons` | POST | `stack.view` | Reverse-parse YAML → `AddonsConfig` (wizard bootstrap) |
| `/api/compose-stack/host-addons` | GET | `stack.view` | Compute addon discovery for a host (docker-inspect + file extract) |
| `/api/host/addon-extract-get` | GET | `host.view` | Read the host's curated `AddonConfigExtract` |
| `/api/host/addon-extract-save` | POST | `host.edit` | Persist a sub-tree |
| `/api/host/addon-extract-clear` | POST | `host.edit` | Clear a sub-tree or the entire blob |
| `/api/compose-stack/registry-cache-preview` | POST | `stack.view` | Dry-run the rewriter |
| `/api/compose-stack/registry-cache-warm` | POST | `stack.deploy` | Pre-pull rewritten refs |

Traefik / Sablier / Watchtower / Backup do **not** have dedicated
endpoints — they live inside the Save/Deploy DTO under `addons`.

---

## File reference

- `biz/compose_stack_addons_yaml.go` — `AddonsConfig`, prefix map,
  `injectAddonLabels`, `extractAddonConfig`, `detectActiveAddons`,
  `applyResources`, per-addon `buildXxxLabels` + `xxxCfgFromLabels`.
- `biz/compose_stack_addons_discovery.go` — `AddonDiscoveryBiz.Discover`
  for the docker-inspect + file extract merge.
- `biz/host_addon_extract.go` — `AddonConfigExtract` /
  `TraefikExtract` / `GenericAddonExtract` / `RegistryCacheExtract`.
- `biz/compose_stack.go` — Save / Deploy / Stop (compose-down) /
  Start logic; `LastWarnings`.
- `api/compose_stack.go` — Save/Deploy/ParseAddons/HostAddons +
  RegistryCachePreview/Warm handlers.
- `api/host.go` — AddonExtractGet/Save/Clear + RegistryCacheGet/Save.
- `ui/src/components/stack-addons/` — `AddonTabTraefik.vue`,
  `AddonTabSablier.vue`, `AddonTabWatchtower.vue`,
  `AddonTabBackup.vue`, `AddonTabResources.vue`,
  `AddonTabRegistryCache.vue`, `LabelPreview.vue`,
  `StructuredLabelsEditor.vue`.
- `ui/src/components/host-addons/` — `HostAddonTraefik.vue`,
  `HostAddonGeneric.vue`, `HostAddonRegistryCache.vue`.
- `ui/src/utils/stack-addon-schemas.ts` — per-addon row schemas.
- `ui/src/pages/compose_stack/Edit.vue` — tab host + gating.
