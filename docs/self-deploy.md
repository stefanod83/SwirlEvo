# Self-deploy

Swirl can redeploy itself. The v3 flow is:

1. Register the compose stack that runs Swirl as a **Swirl-managed compose stack** (via the `Stacks` page).
2. Flag it as the self-deploy source in **Settings → Self-deploy**.
3. When you open that stack's Edit page, the **Deploy** button becomes **Auto-Deploy** (warning-colored). Click it to trigger a short-lived sidekick container that stops the running Swirl, pulls the new image, redeploys the stack, and verifies the new Swirl is healthy.

This page is the operator guide. The feature is **standalone mode only**.

---

## Table of contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Getting started](#getting-started)
4. [Subsequent deploys via the UI](#subsequent-deploys-via-the-ui)
5. [Live progress + session survival](#live-progress--session-survival)
6. [Recovery mode + stuck lock](#recovery-mode--stuck-lock)
7. [Rollback](#rollback)
8. [Preflight validation](#preflight-validation)
9. [Troubleshooting](#troubleshooting)
10. [Security considerations](#security-considerations)
11. [Limitations](#limitations)

---

## Overview

| Component | Role |
|-----------|------|
| **Source ComposeStack** | A Swirl-managed compose stack whose YAML describes the currently-running Swirl + its siblings (mongodb, etc.). Lives in Swirl's own DB. |
| **Main Swirl** | Validates the source YAML, parses `.env`-style variables, writes `job.json` on the shared `/data/self-deploy/` volume, and spawns the sidekick. |
| **Sidekick container** (`swirl-deploy-agent-<short>`) | Runs with `network_mode: host`. Stops + renames old primary, pulls image, calls `StandaloneEngine.DeployWithResult` on the stack's project name (preserving the renamed backup via `PreserveContainerNames`), health-checks the new Swirl by resolving its container IP from compose labels, then removes the backup. Exits when the deploy resolves. |
| **Shared volume** (`/data/self-deploy/`) | Contains `job.json`, `state.json`, `.lock`. Survives across container swaps **only** if the Swirl service in the source YAML mounts a persistent volume at `/data`. A preflight check blocks the deploy if this mount is missing. |

### The rename-then-deploy pivot

Self-deploy *never removes* the previous Swirl container until the new one is fully healthy:

1. Inspect the primary to capture its original container name.
2. Graceful `docker stop` + **rename** to `swirl-previous` (not remove). Labels are preserved.
3. Pull the target image.
4. `StandaloneEngine.DeployWithResult(project, yaml, opts)` with `opts.PreserveContainerNames=["swirl-previous"]`. The engine recreates all services in the stack **except** the renamed backup. Networks/volumes declared in YAML are reused when already present.
5. Resolve the new Swirl container's IP by compose labels (`com.docker.compose.project=<stack>` + `com.docker.compose.service` containing `swirl`). Poll `http://<ip>:<expose-port>/api/system/mode` until 200 OK or the health budget expires. The URL is **re-resolved per probe** so an in-flight container restart (new IP) is tolerated.
6. On success: remove `swirl-previous`. On failure: `rollback()` (see below).

### Why `PreserveContainerNames` matters

The renamed backup still carries the `com.docker.compose.project=<stack>` label. Without preservation, `removeProjectContainers` at the top of `DeployWithResult` would destroy the backup before the deploy is even attempted — leaving no rollback target. The sidekick passes `["swirl-previous"]` both during deploy and during rollback cleanup.

---

## Prerequisites

- **Standalone mode.** Swarm mode is blocked at the biz level.
- **A Swirl-managed compose stack for this Swirl instance.** Register via **Stacks → Import** or **Stacks → New**.
- **A persistent `/data` volume.** The Swirl service YAML **must** declare a mount at `/data`, e.g. `swirl-data:/data` with a top-level `volumes: swirl-data:` entry. Without this the self-deploy state is lost on every restart → second deploy shows "No logs", lock flip-flops between containers. The preflight blocks deploys that are missing this mount.
- **Self-identification.** Swirl must know its own container ID. Priority order in `SelfContainerID()`:
  1. `SWIRL_CONTAINER_ID` env var (if set and non-empty).
  2. `/proc/self/cgroup` parsing (standard Docker runtime).
  3. `os.Hostname()` fallback (Docker sets hostname to short container ID by default).

  **Do NOT set `SWIRL_CONTAINER_ID=${HOSTNAME}` in your YAML**: the compose parser expands `${HOSTNAME}` against the Swirl daemon's process environment, which usually doesn't carry `HOSTNAME`, leaving the variable empty. Let the fallback chain handle it.
- **Docker socket mount.** `/var/run/docker.sock:/var/run/docker.sock` on the primary.
- **External networks must exist.** If your YAML references `external: true` networks (e.g. `traefik-net`), they must already exist on the daemon — `preflightExternalNetworks` bails otherwise.
- **Env file syntax is respected.** The source stack's EnvFile (`.env`) is parsed and its vars are injected before YAML parse, both primary-side (for preflight) and sidekick-side (for the actual deploy). `${VAR}` references in volumes/ports/env resolve as expected.

---

## Getting started

Example compose YAML for a Swirl instance fronted by Traefik + backed by MongoDB:

```yaml
services:
  mongodb:
    image: mongo:latest
    restart: unless-stopped
    expose:
      - 27017
    volumes:
      - mongo-data:/data/db
    networks:
      - swirlevo-net

  swirl:
    image: registry.devarch.local:443/devarch-it/swirlevo:v2.0.0rc1
    restart: unless-stopped
    environment:
      - MODE=standalone
      - DB_TYPE=mongo
      - DB_ADDRESS=mongodb://mongodb:27017/swirl
      - SWIRL_BACKUP_KEY=${SWIRL_BACKUP_KEY}
      - SWIRL_BACKUP_DIR=${SWIRL_BACKUP_DIR}
      - TZ=Europe/Rome
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.swirlevo.rule=Host(`swirl.example.com`)"
      - "traefik.http.routers.swirlevo.entrypoints=https"
      - "traefik.http.routers.swirlevo.tls=true"
      - "traefik.http.services.swirlevo.loadbalancer.server.port=8001"
    volumes:
      - swirl-data:/data
      - /var/run/docker.sock:/var/run/docker.sock
      - /opt/DockerData/swirl/backup:${SWIRL_BACKUP_DIR}
    networks:
      - swirlevo-net
      - traefik-net
    depends_on:
      - mongodb

volumes:
  mongo-data:
  swirl-data:

networks:
  traefik-net:
    driver: overlay
    external: true
  swirlevo-net:
    driver: bridge
```

Corresponding `.env`:
```
SWIRL_BACKUP_KEY=<your-backup-key>
SWIRL_BACKUP_DIR=/data/backup
```

Then:

1. Register this YAML as a managed stack in the **Stacks** page (Import or New).
2. Open **Settings → Self-deploy**:
   - Flip **Enabled**.
   - Pick the stack from the **Source stack** dropdown.
   - Optional: tune **Auto-rollback**, **Deploy timeout**, **Recovery port**, **Recovery allow-list**.
   - Click **Save**.
3. Navigate to **Stacks → that stack → Edit**. The **Deploy** button is now yellow and labeled **Auto-Deploy**.
4. Change the `image:` tag in the YAML editor to the new version, click **Save**, then **Auto-Deploy**.
5. A modal opens with a live progress iframe served by the sidekick on `<recovery-port>`.
6. When the new Swirl answers `GET /api/system/mode`, the modal closes and the page full-reloads on `/`.

---

## Subsequent deploys via the UI

Every future deploy is: **edit the image tag in the stack YAML → click Auto-Deploy**.

There is no separate "template editor" in Settings — the YAML is the single source of truth, edited via the normal compose-stack editor.

---

## Live progress + session survival

During a deploy the following guardrails are active:

- **`selfDeployInProgress` flag** (Vuex + sessionStorage, 10-minute TTL): set when Auto-Deploy opens the progress modal.
- **Axios interceptor** ([ui/src/api/ajax.ts](../ui/src/api/ajax.ts)): while the flag is true, transient 401/403/404/500 responses are **silenced** (resolved with a synthetic `{code:-1}` body rather than a never-resolving promise) so:
  - The operator is NOT redirected to login when the old Swirl's HTTP server shuts down.
  - No memory leak from accumulating pending async closures.
- **Router guard**: while the flag is true, any navigation away from `setting`/`login`/`init` is bounced back to `setting` so the progress modal stays visible.
- **Session restore**: if the tab full-reloads mid-deploy, the Settings page checks sessionStorage on mount and re-opens the modal via `resumeFromSession()`.
- **Polling**: the modal polls `GET /api/system/mode` every 3 seconds via plain `fetch` (bypasses the axios interceptor). First `200 OK` triggers `onDeploySuccess` which clears the flag and reloads the page.

---

## Recovery mode + stuck lock

### Stale lock detection

If a previous deploy crashes mid-flight (Swirl OOM, sidekick segfault, host reboot), the `.lock` file stays but the sidekick is gone. Two mechanisms recover:

1. **Boot-hook reclaim** (in `NewSelfDeploy`): on every Swirl startup, a best-effort goroutine calls `reclaimStaleLock()`:
   - If `.lock` exists AND state.json says we're in an in-progress phase AND the expected sidekick container (`swirl-deploy-agent-<short>`) is NotFound / exited / dead → remove lock, rewrite state as `Failed("abandoned: …")`.
   - Refuses to touch a running sidekick (the real thing is doing its job).
2. **Pre-trigger reclaim**: `TriggerDeploy` calls the same function before `acquireSelfDeployLock`, so a new deploy never sees a stale lock from a just-crashed previous attempt.

### Sidekick watchdog

90 seconds after `spawnSidekick` returns, a goroutine checks: if the phase is still `Pending`, the sidekick is declared dead, state is rewritten as `Failed("sidekick did not report status within 90s — check container logs of <name>")`, and the lock is removed so a retry is possible without operator intervention.

### Clear stuck lock (manual)

If for some reason the automatic recovery misses a stale state, Settings → Self-deploy shows a **Clear stuck lock** button whenever `status.canReset` is true (set by the backend when the on-disk phase is in-progress but the sidekick is missing/dead). The button hits `POST /api/self-deploy/reset` which:

- Refuses with `ErrSelfDeployBlocked` if the sidekick is actually running.
- Otherwise runs `reclaimStaleLock()` and emits an audit event (`SelfDeployReset`).

---

## Rollback

### Automatic rollback

Enabled by default. On any deploy failure after the rename step:

1. `engine.RemoveExcept(stackName, false, ["swirl-previous"])` — tears down the partially-started new stack project while preserving the backup.
2. If a leftover container under the primary's original name exists, remove it.
3. Rename `swirl-previous` back to the original name.
4. Start it + health-check against it (30s budget, via the same label-based URL resolver).

If rollback itself fails, the state transitions to `recovery` and the sidekick keeps serving its HTTP server for manual intervention.

### Manual rollback (last resort)

```bash
docker ps -a --filter name=swirl
docker rm -f <new-swirl-container-name>
docker rename swirl-previous <original-name>
docker start <original-name>
```

---

## Preflight validation

Before the sidekick is spawned, `prepareJob` runs a battery of checks — any failure returns `ErrSelfDeployBlocked` (HTTP 500 body `{"code":1007, "info":"..."}`) without touching the running containers:

1. **Standalone mode.** Refuses under Swarm.
2. **Self-identification.** `SelfContainerID()` must resolve.
3. **Feature enabled + source stack selected.**
4. **Source stack exists + has compose content.**
5. **YAML parseable** (with env file interpolation).
6. **Docker daemon reachable** (`Ping`).
7. **Daemon-aware invariants**:
   - Primary container exists.
   - External networks and volumes exist.
   - `RecoveryPort ≠ ExposePort`.
   - No service uses `container_name` colliding with `swirl-deploy-agent-*`.
8. **Stack compatibility** (`checkStackCompatibility`):
   - For every env var on the primary that looks like `scheme://host:port`, if `host` matches a target service name, the Swirl service and that service must share at least one network in the target YAML. Example blocker:
     > `env DB_ADDRESS references service "mongodb" but "swirl" and "mongodb" share no network in the target YAML`
9. **Persistent `/data` volume**:
   > `service "swirl" does not declare a persistent volume at /data — add volumes: [<name>:/data] and a top-level volumes: entry, otherwise self-deploy state is lost on every restart`

---

## Troubleshooting

### Deploy returns 500 immediately

Check the response body. Coded errors (`1007`) are preflight blocks — read the `info` field; it tells you exactly what is missing:

- `"self-deploy is not enabled"` — flip Enabled in Settings.
- `"no source stack configured"` — pick a stack.
- `"service swirl does not declare a persistent volume at /data"` — add the volume mount.
- `"env X references service Y but swirl and Y share no network"` — fix the YAML network attachments.
- `"a self-deploy is already in progress"` — either wait for the active sidekick, or click **Clear stuck lock** if the sidekick is dead.

### Status stuck on "Pending" forever

Check the sidekick container:
```bash
docker ps -a --filter name=swirl-deploy-agent --format "table {{.Names}}\t{{.Status}}"
docker logs swirl-deploy-agent-<short>
```
If it died early, the Setting page will show `Sidekick container … exited` + the container logs inline. The 90s watchdog will eventually mark the job Failed and free the lock.

### "Bad Gateway" in the browser after deploy succeeds

Transient — Traefik needs ~5-15s to re-discover the new container on `traefik-net`. If it persists:
- Verify the new swirl container actually joined `traefik-net`: `docker inspect <new-container> --format '{{json .NetworkSettings.Networks}}'`.
- Verify Traefik is watching Docker events (provider config) and the labels on the new container are identical to the old.

### "No logs" + Status Idle after a deploy

The `/data` volume isn't persistent → the new container lost `state.json`. Add `swirl-data:/data` to the YAML and retry. The preflight now blocks this scenario, but an old deploy done before the preflight was added can still leave state behind.

### `${VAR}` appears empty in the rendered compose

The variable isn't in the EnvFile on the source stack, or the EnvFile wasn't saved. Check the stack's Env File editor.

### Health check fails with `connection refused on 127.0.0.1:8001`

Pre-fix: the sidekick probed `127.0.0.1:<exposePort>` on its host network, which only works if the YAML publishes the port to the host. With Traefik-fronted setups (`ports:` commented out), this always failed.

Post-fix: the sidekick resolves the target container's IP via `com.docker.compose.project + com.docker.compose.service` labels and probes `http://<container-ip>:<port>`. If you still see the fallback `127.0.0.1` in logs, the container couldn't be found by label — make sure `StandaloneEngine` is actually applying the project label on create.

---

## Security considerations

- **Sidekick HTTP server** guarded by IP allow-list + per-session CSRF token. Default allow-list: `127.0.0.1/32`.
- **Docker socket mount** on the sidekick is equivalent to root on the host. Attack surface is the 3-endpoint HTTP server.
- **Recovery port NOT auto-published** — loopback only by default. Widen via `RecoveryAllow` or via SSH tunnel.
- **Audit events** emitted for every lifecycle transition: `SelfDeployStart`, `SelfDeploySuccess`, `SelfDeployFailure`, `SelfDeployReset`.

---

## Limitations

- **Standalone only.** Swarm mode is blocked.
- **No volume copy.** The new containers attach to the existing named volumes via qualified name `<project>_<volume>`.
- **Single-host.** Sidekick orchestrates containers on the host where Swirl runs.
- **Source stack is mandatory.** No "build-from-scratch" mode — the stack must be registered first.
- **`build:` not supported.** The YAML must reference pre-built images via `image:`.
- **Container name display strips leading slash.** Swirl's API returns container names without Docker's internal `/` prefix.

---

## See also

- `CHANGELOG.md` — the v3 simplification + hardening entries.
- `biz/self_deploy.go` — `TriggerDeploy`, `prepareJob`, `checkStackCompatibility`, `reclaimStaleLock`, `ResetLock`.
- `cmd/deploy_agent/lifecycle.go` — `runDeploy`, `deployNew`, `rollback`, `resolveHealthURL`.
- `docker/compose/standalone.go` — `DeployOptions.PreserveContainerNames`, `RemoveExcept`, DNS-alias-on-shorthand fix.
