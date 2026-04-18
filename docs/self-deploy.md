# Self-deploy

Swirl can redeploy itself. You push a new Swirl image to a registry,
click **Deploy now** in Settings → Self-deploy, and a short-lived
sidekick container stops the running Swirl, pulls the new image, starts
the new Swirl, and verifies it is healthy. During the deploy a live
progress view (served by the sidekick) streams phase + logs straight
into the main Swirl UI; on failure the same sidekick exposes a recovery
UI where you can retry or rollback.

This page is the operator guide. The feature is scoped to **standalone
mode** — see *Limitations* at the bottom before planning your rollout.

---

## Table of contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Getting started](#getting-started)
4. [Subsequent deploys via the UI](#subsequent-deploys-via-the-ui)
5. [Live progress](#live-progress)
6. [Recovery mode](#recovery-mode)
7. [Rollback](#rollback)
8. [Troubleshooting](#troubleshooting)
9. [Security considerations](#security-considerations)
10. [Limitations (v1.1)](#limitations-v11)

---

## Overview

| Component | Role |
|-----------|------|
| **Main Swirl** | Hosts the UI, auto-populates the self-deploy placeholders from the current container, renders the compose template, writes `job.json` to the shared volume, spawns the sidekick container. |
| **Sidekick container** (`swirl-deploy-agent-<short>`) | Runs the lifecycle: stop + rename old container, pull new image, deploy new stack, health-check, succeed or enter recovery. Serves the live-progress HTTP view from the very start of the deploy (not only on failure). Has access to the Docker socket and the self-deploy state directory, nothing else. |
| **Shared volume** (`/data/self-deploy/`) | Contains `job.json`, `state.json`, a `.lock` file, and a rotated `history/` folder. Lives on the same volume as Swirl's BoltDB. |

The sidekick is *not* persistent — it exits when the deploy is over
(success: exit 0; recovery completed: exit 0; gave up: exit 3). The
sidekick container itself is NOT removed on exit (`AutoRemove=false`)
so you can read its logs after the fact.

### The rename-then-deploy pivot

Self-deploy *never removes* the previous Swirl container until the new
one is fully healthy. Sequence:

1. Stop the primary with a graceful timeout.
2. **Rename** it to `swirl-previous` (not remove).
3. Pull the new image.
4. Deploy the new stack (which creates a *new* container named `swirl`).
5. Wait for `GET /api/system/mode` to return 200.
6. Remove `swirl-previous` to reclaim resources.

If any step fails and **auto-rollback** is on, the sidekick tears down
the new stack, renames `swirl-previous` back to `swirl`, starts it, and
waits for it to answer the health check. Worst case: you are back on
the version you started with, plus one warning in the audit log.

---

## Prerequisites

- **Standalone mode.** Swarm mode is blocked at the biz level — the UI
  button is hidden and the API returns a coded error.
- **Persistent data volume.** Recommended but **not pre-created**: the
  rendered compose declares `swirl_data` as a compose-managed named
  volume. Docker creates it on first `up` and preserves it across
  redeploys automatically. No `docker volume create` step is required.
- **Dedicated network.** Recommended but **not pre-created**: the
  rendered compose declares `swirl_net` as a compose-managed bridge
  network. Compose creates it on first `up` and reuses it on every
  subsequent redeploy. No `docker network create` step is required.
- **Self-identification.** Swirl must know its own container ID so
  the sidekick can swap the right container out. Two options:
  - Set `SWIRL_CONTAINER_ID=$(hostname)` in the environment
    (Docker sets the in-container hostname to the container ID by
    default, so this Just Works in most cases).
  - Or rely on `/proc/self/cgroup` parsing — works automatically in
    most Docker runtimes. On podman, Kubernetes, or other runtimes
    where cgroup parsing fails, set `SWIRL_CONTAINER_ID` explicitly.
- **Docker socket mount.** `/var/run/docker.sock:/var/run/docker.sock`
  on the primary. The sidekick inherits the mount when Swirl spawns it.
- **Recovery port reachable.** The sidekick binds the live-progress +
  recovery UI on `<RecoveryPort>` (default 8002), gated by IP allow-
  list. The operator's browser must either reach that port directly
  (LAN IP in the allow-list) or via an SSH tunnel (loopback default).

---

## Getting started

The preferred zero-step bootstrap is a single `docker run` — volumes
and networks are created by the rendered compose on first deploy, so
there is nothing to pre-create.

```bash
docker run -d \
  --name swirl \
  -p 8001:8001 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e SWIRL_CONTAINER_ID=$(hostname) \
  cuigh/swirl:<tag>
```

Then:

1. Open `http://<host>:8001` and log in (default admin/admin on first
   boot; change the password immediately).
2. Navigate to **Settings → Self-deploy**.
3. Observe that the placeholders are **already pre-populated** with
   values read from the current container:
   - `ImageTag` — the image reference the container was started with.
   - `ExposePort` — the first `<port>/tcp` host binding (here `8001`).
   - `ContainerName` — the container's name (`swirl`).
   - `VolumeData` — the named volume mounted at `/data`, if any.
   - `NetworkName` — the first non-default attached network.
   - `TraefikLabels` — any `traefik.*` labels present on the container.
4. The **Compose template editor is hidden** by default. Toggle
   **Advanced: edit raw template** only if you need to customise the
   generated YAML beyond the placeholder fields.
5. Tick **Enabled** and click **Save**.
6. Click **Deploy now** and confirm.
7. A modal opens with a live progress iframe streaming phase + logs
   from the sidekick (auto-refresh every 3 s).
8. On success the modal closes and the page full-reloads on the new
   Swirl. On failure, see [Recovery mode](#recovery-mode).

As an alternative, operators who prefer a compose-based bootstrap can
use the shipped
[`compose.self-stack.yml.example`](../compose.self-stack.yml.example)
— it reproduces the same single-`docker run` setup in declarative form
and is a useful reference for Traefik / custom port configurations.

---

## Subsequent deploys via the UI

Because placeholders are auto-populated on the first load after every
new deployment, the normal flow is short:

1. Navigate to **Settings → Self-deploy**.
2. Confirm **Enabled** is on.
3. Update **Target image** to the new tag (e.g. `cuigh/swirl:v2.1.1`).
4. Review the other placeholders (they carry over from the current
   deployment):
   - `ExposePort` / `RecoveryPort` — change only when reconfiguring the
     host.
   - `RecoveryAllow` — list of CIDRs that may reach the live-progress
     + recovery UI. Defaults to `127.0.0.1/32` (loopback only). Widen
     to your admin VPN range to allow remote visibility of the deploy.
   - `TraefikLabels` — if you front Swirl with Traefik, listed labels
     are attached verbatim to the rendered service.
   - `ExtraEnv` — free-form environment variables merged into the
     service.
5. Optional: flip **Advanced: edit raw template** on to edit the raw
   compose file. Most users should not need this — the fields above
   generate a valid template automatically.
6. Click **Save**.
7. Click **Preview YAML** to inspect the rendered compose file (works
   with or without Advanced mode; always shows the final YAML that
   will be handed to the sidekick).
8. Click **Deploy now** and confirm.

UI layout at a glance:

```
Settings → Self-deploy
┌──────────────────────────────────────────────────────────────┐
│  [ ] Enabled                              [Save] [Deploy now]│
├──────────────────────────────────────────────────────────────┤
│  Placeholders                                                │
│    ImageTag       [ cuigh/swirl:v2.1.0                    ]  │
│    ExposePort     [ 8001 ]                                   │
│    RecoveryPort   [ 8002 ]                                   │
│    RecoveryAllow  [ 127.0.0.1/32                          ]  │
│    ContainerName  [ swirl                                 ]  │
│    VolumeData     [ swirl_data                            ]  │
│    NetworkName    [ swirl_net                             ]  │
│    TraefikLabels  [                                        ] │
│    ExtraEnv       [                                        ] │
│                                                              │
│  [ ] Advanced: edit raw template                             │
│    ┌──────────────────────────────────────────┐              │
│    │ ...YAML editor (hidden unless Advanced)..│              │
│    └──────────────────────────────────────────┘              │
│                                       [Preview YAML]         │
└──────────────────────────────────────────────────────────────┘
```

---

## Live progress

Clicking **Deploy now** triggers this sequence in the browser:

1. The backend validates the config, writes `job.json`, spawns the
   sidekick, and responds `202 Accepted` with `{ jobId, recoveryUrl }`.
2. The UI opens a modal with an iframe pointed at `recoveryUrl`
   (typically `http://<host>:8002/`).
3. The iframe renders the sidekick's HTML view, which auto-refreshes
   every 3 s and streams:
   - the current phase (`pending → stopping → pulling → starting →
     health_check → success` or `recovery`);
   - the last N lines of the sidekick log buffer.
4. In parallel, the parent tab polls `GET /api/system/mode` on the
   expected new Swirl endpoint.
5. On the first `200 OK` from the new Swirl the modal closes and the
   page performs a full reload on `/`. The operator lands in the
   freshly-deployed UI.

### If the iframe cannot load

If the browser cannot reach the sidekick (IP not in `RecoveryAllow`,
firewall, reverse-proxy blocking cross-port, …) the iframe shows blank
or "refused to connect". The parent polling still runs, so the deploy
can complete and the page still reloads when the new Swirl answers.
A fallback message in the modal header links to `recoveryUrl` in case
you want to open it in a new tab.

### Timeout behaviour

After 5 minutes (slightly longer than `DeployTimeout`) the parent
displays a warning in the modal header but keeps the view open. The
deploy is still running in the background and the sidekick continues
to serve logs; you can leave the modal open to watch the final phases
or close it and reload manually once the new Swirl is reachable.

---

## Recovery mode

If the new Swirl does not come up healthy within the timeout (default
5 minutes) AND **Auto-rollback** is off — or if auto-rollback itself
failed — the sidekick transitions to **recovery mode**:

- State phase becomes `failed`, `recovery`, or `rolled_back`; Swirl's
  audit log records a `Failure` event.
- The sidekick keeps running and keeps binding the HTTP server on
  `<RecoveryPort>` (by default 8002) — remember the server is up
  **from the start of the deploy**, not only on failure.
- The browser view (served through the same iframe or opened
  directly) now shows the **Retry** / **Rollback** buttons, which are
  hidden during in-progress phases.

The recovery UI offers:

- **Retry** — re-run the same lifecycle with the same image tag (use
  it when you pushed a fixed image under the same tag).
- **Rollback** — re-run the lifecycle with `PreviousImageTag` as the
  target. Puts you back on the version you started from.
- **Download logs** — plain-text dump of the sidekick's log buffer.

Auth model: **IP allow-list + CSRF token**. The recovery UI never sees
a password. The token is generated at sidekick start, bound to the
session cookie, and validated on every POST.

### When are the action buttons visible?

| Phase | Buttons |
|-------|---------|
| `pending`, `stopping`, `pulling`, `starting`, `health_check` | **Hidden** — progress-only view. |
| `success` | Hidden — sidekick is exiting. |
| `failed`, `recovery`, `rolled_back` | Retry + Rollback + Download logs. |

---

## Rollback

### Automatic rollback

Enabled by default (Settings → Self-deploy → Advanced → Automatic
rollback). On any deploy failure:

1. Tear down the partially-started new stack.
2. Remove any leftover `swirl` container that slipped through.
3. Rename `swirl-previous` back to `swirl`.
4. Start it.
5. Health-check it (short timeout — it was already healthy).

If rollback itself fails, the state transitions to `recovery` and the
recovery UI (same URL you saw in the live-progress modal) stays up.
The audit trail records both the deploy failure and the rollback
failure.

### Manual rollback (recovery UI)

If you disabled auto-rollback or the auto-rollback itself failed, use
**Rollback** in the recovery UI. The button is only visible when the
phase is `failed`, `recovery`, or `rolled_back` — never during an
in-progress deploy.

### Manual rollback (last resort)

If the sidekick itself crashed with no recovery UI, you can do it by
hand:

```bash
# See what's around
docker ps -a | grep -E 'swirl(-previous)?'

# Drop the broken new one (if present)
docker rm -f swirl

# Rename the old one back
docker rename swirl-previous swirl

# Start it
docker start swirl
```

---

## Troubleshooting

### Log locations

| What | Where |
|------|-------|
| Sidekick logs | `docker logs swirl-deploy-agent-<short>` — the short-id is the first 8 chars of the job ID, also visible in `state.json`. |
| Deploy state | `/data/self-deploy/state.json` inside the primary Swirl container, or via the Self-deploy panel's **Recent logs** section. |
| Job descriptor | `/data/self-deploy/job.json`. |
| History | `/data/self-deploy/history/<job-id>/` — one directory per past deploy, FIFO-capped at 20. Contains the `job.json` + `state.json` at the time the deploy ended. |
| Swirl audit log | Navigate to **Events**; filter on `Type=SelfDeploy`. Three entries per deploy: `Start`, then `Success` or `Failure`. |

### Common failures

- **"Self-deploy is not enabled"** — you saved the config but left
  Enabled off. Tick it and save again.
- **"Placeholders are empty after first boot"** — Swirl could not
  auto-detect the current container. Either the `SWIRL_CONTAINER_ID`
  env var is missing AND `/proc/self/cgroup` is unreadable (common
  on podman or Kubernetes), or the Docker daemon was unreachable at
  the moment the settings panel was loaded. Set `SWIRL_CONTAINER_ID`
  explicitly and reload the panel.
- **"Iframe shows blank or 'refused to connect'"** — your browser IP
  is not in `RecoveryAllow`. Default is `127.0.0.1/32`, which works
  from SSH tunnels / localhost but not from LAN. Widen the CIDR to
  your admin network and re-save (the change applies to the next
  deploy; the current one keeps the CIDR it launched with).
- **"Deploy stays in `starting` phase forever"** — the new container
  failed to boot. Inspect it via `docker logs swirl` on the host.
  Typical causes: missing volume, bad `DB_ADDRESS`, or bind-mount
  permission issues. Verify `/api/system/mode` is reachable from
  inside the container.
- **"Swirl cannot identify its own container"** — `SWIRL_CONTAINER_ID`
  env var is missing AND `/proc/self/cgroup` parsing failed. Re-deploy
  the primary with `SWIRL_CONTAINER_ID=$(hostname)` set.
- **"external network X referenced in compose does not exist"** —
  only happens if you edited the template in Advanced mode and added
  `external: true` manually. Either drop the flag (let compose manage
  the network) or `docker network create X` on the host, then retry.
- **"Docker daemon not reachable"** — check the socket mount in the
  primary's compose. `PrepareJob` fails before spawning the
  sidekick to give you an actionable message.
- **"A self-deploy is already in progress"** — a previous deploy
  crashed without cleaning the lock file. Inspect
  `/data/self-deploy/.lock`; if the named sidekick container is no
  longer running, delete the file manually.
- **"I want the v1 behaviour with external volumes / networks"** —
  flip **Advanced: edit raw template** on, edit the rendered compose,
  add `external: true` under the volume/network declarations, and
  `docker volume create swirl_data && docker network create swirl_net`
  on the host. The pre-flight checks in biz remain backward-
  compatible: they still validate `external: true` entries when
  present.

---

## Security considerations

### Recovery / live-progress UI exposure

The sidekick HTTP server is guarded by **IP allow-list + per-session
CSRF token**, not by a password. The allow-list defaults to
`127.0.0.1/32`.

- `RecoveryAllow = 0.0.0.0/0` is accepted (the planning brief calls
  it out — operators sometimes know what they are doing behind an
  external firewall) but Swirl logs a WARN line every time the
  sidekick starts with that configuration. **Do not ship this into
  production.** Treat `0.0.0.0/0` as an emergency-only setting.
- The sidekick binds on the **host network namespace**, so whatever
  allow-list you set applies to the host's IPs as the sidekick sees
  them — NOT to the origin behind a reverse proxy. If you front Swirl
  with Traefik/nginx, the recovery UI is usually best kept on
  loopback + SSH tunnel.
- The live-progress server is up **for the entire deploy**, not just
  on failure. If you expose the recovery port publicly, anyone in the
  CIDR allow-list can see the phase + logs during a deploy. Actions
  (Retry / Rollback) remain hidden during in-progress phases, so the
  attack surface during a successful deploy is "log disclosure only".

### Cross-origin iframe

The live-progress iframe loads from a different port (`:8002`) than
the main Swirl (`:8001`). Browsers treat this as a **different
origin**. The iframe loads fine (cross-origin iframe rendering is
permitted) but `postMessage` between parent and iframe uses
`targetOrigin='*'` with the listener validating the payload shape to
mitigate cross-origin injection.

The CSRF token protecting **POST** actions (Retry / Rollback) is
unchanged from v1: it is generated at sidekick start, bound to the
session cookie, and validated on every POST.

### Docker socket access

The sidekick mounts `/var/run/docker.sock`. This is equivalent to root
on the host. The sidekick's attack surface is:

- a 3-endpoint HTTP server (root, logs, retry/rollback) gated by the
  allow-list and CSRF;
- no SSH, no shell, no file-upload;
- exits as soon as the deploy resolves (success or operator action).

Treat the sidekick the same way you treat the primary: do not expose
the socket mount via a public LAN port.

### Port exposure

The recovery port is NOT exposed in the example compose file — you
have to add a `ports:` mapping yourself (or use `--publish` when
launching the sidekick). The default binding is `127.0.0.1:8002`.

---

## Limitations (v1.1)

- **Standalone only.** Swarm mode is blocked at the biz level. Self-
  deploying a Swarm cluster is a multi-node orchestration problem with
  a different failure model — planned for v2.
- **No volume copy.** The new container attaches to the same volumes
  as the old one. There is no "copy-on-redeploy" and therefore no way
  to test a new Swirl against a scratch volume through the UI. Data
  is NOT copied on migrations across hosts.
- **`build:` not supported.** The compose template must reference
  pre-built images via `image:`. This is the A1 strict validation
  mode inherited from the standalone engine — the sidekick does not
  call `ImageBuild`.
- **`depends_on.condition` ignored.** Compose's
  `depends_on.condition: service_healthy` is not honoured; service
  start ordering depends on the order of the `services:` map.
- **Single-host.** The sidekick only orchestrates containers on the
  host where Swirl runs. To self-deploy a multi-host deployment, each
  host runs its own Swirl + sidekick pair.
- **Live progress iframe requires browser reachability.** The iframe
  needs the operator's browser to reach the sidekick's recovery port.
  If the CIDR allow-list blocks the request, the fallback polling on
  `/api/system/mode` still completes the deploy — you just do not see
  live logs in the modal.
- **No canary / blue-green.** The lifecycle is stop-then-start. No
  traffic overlap between the old and new Swirl. Sessions drop during
  the swap — typically 30-60 seconds.

For v2 we are considering: Swarm support (replicated service update
with rollback), copy-on-redeploy, a readiness endpoint that pings the
DB, and canary mode.

---

## See also

- [`compose.self-stack.yml.example`](../compose.self-stack.yml.example)
  — optional declarative bootstrap (compose-managed volumes + network,
  no `docker volume/network create` needed).
- `CHANGELOG.md` — full list of v1.1 changes (zero-step bootstrap,
  placeholder auto-populate, Advanced toggle, live-progress iframe).
