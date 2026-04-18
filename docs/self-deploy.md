# Self-deploy

Swirl can redeploy itself. The v2 flow is simple: you register the
compose stack that runs Swirl as a Swirl-managed compose stack, select
it in **Settings → Self-deploy**, tweak a couple of scalars (typically
just the target image tag) and click **Deploy**. A short-lived sidekick
container stops the running Swirl, pulls the new image, redeploys the
stack under the same project name, and verifies the new Swirl is
healthy.

This page is the operator guide. The feature is scoped to **standalone
mode**.

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
10. [Limitations](#limitations)

---

## Overview

| Component | Role |
|-----------|------|
| **Source ComposeStack** | A Swirl-managed compose stack whose YAML describes the currently-running Swirl. Lives in Swirl's own DB under the usual compose-stack CRUD (`Stacks` menu in standalone mode). |
| **Main Swirl** | Reads the source stack from the DB, applies placeholder overrides, writes the updated YAML back into the stack record, and spawns the sidekick. |
| **Sidekick container** (`swirl-deploy-agent-<short>`) | Runs the lifecycle: stop + rename old container, pull new image, call `StandaloneEngine.Deploy` on the stack project name, health-check, succeed or enter recovery. Serves the live-progress HTTP view from the very start of the deploy. Exits when the deploy resolves. |
| **Shared volume** (`/data/self-deploy/`) | Contains `job.json`, `state.json`, a `.lock` file. Lives on the same volume as Swirl's BoltDB. |

### Why a source stack?

Anchoring self-deploy to an existing Swirl-managed stack means:

- **No hidden stack.** The deploy updates the same project that appears
  in the `Stacks` list, so nothing is created "off-the-books".
- **No container-inspection heuristics.** The source YAML is the
  authoritative template — we do not try to reverse-engineer volume
  names, network names, or container names from the running container.
- **Mixed edits are preserved.** You can open the stack in the Stacks
  editor, tweak something (e.g. add a service), Save without Deploy,
  then later Import-from-stack in Self-deploy to pick up the change.
- **The project name is stable.** `StandaloneEngine.Deploy` uses the
  stack's persisted name as the compose project, matching every label
  (`com.docker.compose.project=<name>`) on the existing containers so
  the deploy is a true update, not a parallel creation.

### The rename-then-deploy pivot

Self-deploy *never removes* the previous Swirl container until the new
one is fully healthy. Sequence:

1. Inspect the primary to capture its original container name.
2. Stop it with a graceful timeout.
3. **Rename** it to `swirl-previous` (not remove).
4. Pull the new image.
5. Call `StandaloneEngine.Deploy` on the source stack's project name
   with the new YAML. The engine creates fresh containers labelled for
   the same project and named according to the source YAML.
6. Wait for `GET /api/system/mode` to return 200.
7. Remove `swirl-previous` to reclaim resources.

If any step fails and **auto-rollback** is on, the sidekick tears down
the new stack project, renames `swirl-previous` back to its original
name, starts it, and waits for it to answer the health check. Worst
case: you are back on the version you started with, plus one warning
in the audit log.

---

## Prerequisites

- **Standalone mode.** Swarm mode is blocked at the biz level.
- **A Swirl-managed compose stack for the Swirl instance.** If you
  bootstrapped Swirl with `docker run` rather than `docker compose`,
  create a compose YAML that describes it and import/register it via
  **Stacks → New**. Your Swirl container does not have to restart
  for this — the stack record just has to exist.
- **Self-identification.** Swirl must know its own container ID so
  the sidekick can swap the right container out. Two options:
  - Set `SWIRL_CONTAINER_ID=$(hostname)` in the environment.
  - Rely on `/proc/self/cgroup` parsing (works on most Docker runtimes).
- **Docker socket mount.** `/var/run/docker.sock:/var/run/docker.sock`
  on the primary.
- **Recovery port reachable.** The sidekick binds the live-progress +
  recovery UI on `<RecoveryPort>` (default 8002), gated by IP allow-
  list. The operator's browser must reach that port directly (LAN IP
  in the allow-list) or via SSH tunnel (loopback default).

---

## Getting started

1. Make sure Swirl is running as a Swirl-managed compose stack. The
   simplest bootstrap is `docker compose up -d` with a YAML you also
   register in the Stacks list, **or** import the already-running
   project via **Stacks → Import**.
2. Open **Settings → Self-deploy**.
3. In the **Source stack** dropdown, pick the stack that represents
   this Swirl. The options come from `GET /api/compose-stack/search`,
   one entry per managed stack (`<host> / <name>`).
4. Click **Import from stack**. The panel fills with:
   - `Template` — the stack's YAML content.
   - `ImageTag` — extracted from the Swirl service's `image:` field.
   - `ExposePort` — first published port.
   - `DbType` — derived from the `DB_TYPE` env var.
   - `TraefikLabels` — every `traefik.*` routing label.
   - `ExtraEnv` — every env var except the Swirl-managed ones
     (`DB_TYPE`, `DB_ADDRESS`, `SWIRL_CONTAINER_ID`, `MODE`).
5. Tick **Enabled** and click **Save**.
6. Change **Target image** to the new version (e.g. `cuigh/swirl:v2.1.1`).
7. Click **Deploy now** and confirm.
8. A modal opens with a live progress iframe streaming phase + logs
   from the sidekick. On success the modal closes and the page
   full-reloads on the new Swirl.

---

## Subsequent deploys via the UI

The normal flow after the first import:

1. Navigate to **Settings → Self-deploy**.
2. Update **Target image** to the new tag.
3. Optionally flip **Advanced: edit raw template** to tweak the YAML
   directly (useful for adding a new env var to the service without
   going through `ExtraEnv`).
4. Click **Save**. The YAML you just approved is persisted into the
   source ComposeStack record so the next Import reflects your changes.
5. Click **Preview YAML** to inspect the final compose file before the
   sidekick sees it.
6. Click **Deploy now** and confirm.

### Relationship with the Stacks page

The source stack's `Content` field is updated atomically every time
Self-deploy runs — the Stacks page will reflect the new YAML as soon
as the deploy starts. If you edit the stack in the Stacks page
(e.g. to add a service) and want Self-deploy to pick up the change,
click **Import from stack** again to re-seed the Self-deploy form
from the updated YAML.

---

## Live progress

Clicking **Deploy now** triggers this sequence in the browser:

1. The backend validates the config, writes `job.json` (including the
   stack project name and the rendered YAML), persists the rendered
   YAML into the source ComposeStack, spawns the sidekick, and
   responds `202 Accepted` with `{ jobId, recoveryUrl, stackName }`.
2. The UI opens a modal with an iframe pointed at `recoveryUrl`.
3. In parallel, the parent tab polls `GET /api/system/mode` on the
   expected new Swirl endpoint. On the first `200 OK` the modal
   closes and the page performs a full reload on `/`.

---

## Recovery mode

If the new Swirl does not come up healthy within the timeout AND
**Auto-rollback** is off — or if auto-rollback itself failed — the
sidekick transitions to **recovery mode**:

- State phase becomes `failed`, `recovery`, or `rolled_back`; Swirl's
  audit log records a `Failure` event.
- The sidekick keeps running and keeps binding the HTTP server on
  `<RecoveryPort>`.
- The recovery UI shows **Retry** / **Rollback** / **Download logs**
  buttons, gated by IP allow-list + CSRF.

---

## Rollback

### Automatic rollback

Enabled by default. On any deploy failure:

1. Tear down the partially-started new stack project via
   `StandaloneEngine.Remove(stackName)`.
2. Remove any leftover container with the primary's original name.
3. Rename `swirl-previous` back to the primary's original name.
4. Start it and health-check.

If rollback itself fails, the state transitions to `recovery`.

### Manual rollback (last resort)

If the sidekick itself crashed:

```bash
docker ps -a | grep -E 'swirl(-previous)?'
docker rm -f <new-swirl-container-name>  # whatever the rendered YAML named it
docker rename swirl-previous <original-name>
docker start <original-name>
```

---

## Troubleshooting

### Common failures

- **"Self-deploy is not enabled"** — save with Enabled ticked.
- **"No source stack configured"** — pick a stack from the dropdown
  and click Import.
- **"Source stack not found"** — the stack record was deleted after
  import. Pick a different stack and re-import.
- **"Swirl cannot identify its own container"** — set
  `SWIRL_CONTAINER_ID=$(hostname)` in the primary's environment.
- **Deploy stays in `starting` phase forever** — the new container
  failed to boot. Inspect it via `docker logs <container-name>` on
  the host; the container name is whatever the rendered YAML declared.
- **"A self-deploy is already in progress"** — a previous deploy
  crashed without cleaning the lock. Inspect
  `/data/self-deploy/.lock`; if the sidekick container is no longer
  running, delete the file manually.

---

## Security considerations

- The sidekick HTTP server is guarded by **IP allow-list + per-session
  CSRF token**, not by a password. The allow-list defaults to
  `127.0.0.1/32`.
- The sidekick mounts `/var/run/docker.sock` (equivalent to root on
  the host). Attack surface: 3-endpoint HTTP server gated by allow-
  list + CSRF. No SSH, no shell, no file upload.
- The recovery port is NOT auto-published — you have to add a `ports:`
  mapping to the compose YAML yourself. Default binding is loopback.

---

## Limitations

- **Standalone only.** Swarm mode is blocked.
- **No volume copy.** The new containers attach to the same volumes.
- **Single-host.** The sidekick only orchestrates containers on the
  host where Swirl runs.
- **Source stack is mandatory.** There is no "build a template from
  scratch in Self-deploy" mode anymore — register the stack in the
  Stacks list first, then import.
- **`build:` not supported.** The compose template must reference
  pre-built images via `image:`.

---

## See also

- `CHANGELOG.md` — the v2 refactor changelog entry.
