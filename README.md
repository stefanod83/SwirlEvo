# SWIRL

**Swirl** is a web management tool for Docker, supporting both **Swarm cluster** and **Standalone host** modes.

> Version 2.0 introduces dual-mode operation: manage Docker Swarm clusters (existing) or standalone Docker hosts (new) with Portainer-style container lifecycle and compose-stack deployment per host.

## Features

* **Dual mode**: Swarm cluster management OR standalone Docker host management.
* **Standalone hosts**: add remote Docker hosts via Unix socket, TCP, TCP+TLS, or SSH. Host management under `Docker → Hosts`.
* **Global host selector (standalone mode)**: dropdown in the top header; the selection persists across reloads (localStorage) and drives every per-host list page. If only one host is registered, it's selected automatically.
* **Portainer-like containers**: per-host lifecycle — start, stop, restart, pause/unpause, kill, rename, logs, exec, stats, delete. Delete disabled when container is running or paused.
* **Compose stacks for standalone mode**: parse a `docker-compose.yml`, deploy it onto a selected host via the Docker SDK (no external CLI), then manage lifecycle (Deploy / Save / Start / Stop / Remove). Compose-CLI label convention means stacks created outside Swirl are visible and manageable too.
* **External stack import**: discovered stacks get a Details view with reconstructed `docker-compose.yml` (editable in-browser) and an **Import / Import & Redeploy** action to promote them to Swirl-managed. Direct Start/Stop/Remove work even before importing.
* **Images Portainer-style**: `Unused` badge for images not referenced by any container; **Force delete** (red, with confirmation) removes an image from all repositories even when referenced.
* **Volumes**: `Unused` badge when the volume is not mounted by any container.
* Swarm components management (services, tasks, stacks, configs, secrets, nodes).
* Compose parser + deployment (Swarm stacks + standalone stacks).
* Service monitoring based on Prometheus and cAdvisor (Swarm mode).
* Service auto scaling (Swarm mode only).
* LDAP authentication.
* Full permission control based on RBAC.
* i18n: English, Chinese.

## Operating Modes

The `MODE` env var selects which UI and which endpoints are active. Swapping mode does not require a rebuild — only a restart.

### Swarm Mode (default)

Traditional Docker Swarm management. Requires a Swarm with at least one manager reachable by Swirl. Uses socat-agent containers (one per node) for node-scoped operations.

Menu in swarm mode:
```
Home · Swarm (Registries/Nodes/Networks/Services/Tasks/Stacks/Configs/Secrets) · Local (Images/Containers/Volumes) · System
```

### Standalone Mode

No Swarm required. Register Docker hosts via the UI. Per-host container/image/volume management. Compose stacks deployed directly on a host via Docker SDK.

Menu in standalone mode:
```
Home · Docker (Hosts/Registries/Networks/Containers/Stacks) · Local (Images/Volumes) · System
```

Swarm-only endpoints (`/service/*`, `/task/*`, `/config/*`, `/secret/*`) return **404** in standalone mode. The auto-scaler is disabled. The router guard also blocks swarm-only routes from being reached by URL.

Activate with `MODE=standalone`.

## Standalone UX

### Global host selector

In standalone mode, the header (next to the Swirl logo) shows a **Host** dropdown populated from the registered hosts. Its value is shared across all per-host pages via the Vuex store and **persisted in `localStorage`** — so reloading the browser restores the last selection.

Values:

- **All hosts** — visible only when 2+ hosts are registered. Overview pages (Home, Docker → Hosts / Registries, System → *) show cross-host aggregates. Per-host pages (Containers, Stacks, Networks, Images, Volumes) show an **empty prompt** asking the user to select a host.
- **A single host** — every per-host page filters automatically on that host. The Home summary recalculates counters for that host.

Auto-select: if only one host is registered, it's selected automatically and the "All" option is hidden. The selector is hidden entirely in swarm mode (it's only relevant for standalone).

### Importing external stacks

Stacks created outside Swirl (plain `docker compose up -d` on a host) are discovered via the compose-CLI label convention. In the Stacks list they are tagged `external` and get a dedicated **Details** action — the actions Start/Stop/Remove work on them directly too, by `(hostId, name)`.

In the Details view you'll find:

- **Overview**: host, status, services, networks, volumes.
- **Containers**: the live container list with state/ports/created, each linked to the full container detail.
- **Compose (YAML)**: a best-effort reconstruction of the compose file from the running containers (CodeMirror editor). Review and edit if needed.

Then click **Import** (persist only) or **Import & Redeploy** (persist + apply the YAML, fully recreating the containers). After import, the stack becomes Swirl-managed and all the usual actions (Deploy / Save / Edit / Start / Stop / Remove) are available.

The reconstruction is approximate: fields not derivable from a running container are omitted — `build`, `healthcheck` (unless already in container args), `secrets`, `configs`, `deploy`, `depends_on`. The Details banner warns the user to review the YAML before Import & Redeploy.

## Configuration

### Environment Variables

| Name               | Default                          | Description                                   |
|--------------------|----------------------------------|-----------------------------------------------|
| MODE               | swarm                            | Operating mode: `swarm` or `standalone`       |
| DB_TYPE            | mongo                            | Storage engine: `mongo` or `bolt`             |
| DB_ADDRESS         | mongodb://localhost:27017/swirl  | MongoDB URI, or directory path for BoltDB     |
| TOKEN_EXPIRY       | 30m                              | JWT token lifetime                            |
| DOCKER_ENDPOINT    | (from env)                       | Docker daemon endpoint                        |
| DOCKER_API_VERSION | (auto-negotiated)                | Docker API version (optional)                 |
| AGENTS             | (empty)                          | Swarm agent services (swarm mode only)        |

### Config File

All options can be set via `config/app.yml`:

```yaml
name: swirl
banner: false

web:
  entries:
    - address: :8001
  authorize: '?'

swirl:
  mode: swarm        # or "standalone"
  db_type: mongo
  db_address: mongodb://localhost:27017/swirl

log:
  loggers:
  - level: info
    writers: console
  writers:
  - name: console
    type: console
    layout: '[{L}]{T}: {M}{N}'
```

## Deployment

### Standalone — single container, BoltDB (simplest)

No external DB, single volume for persistence.

```bash
docker compose -f docker-compose.standalone-bolt.yml up -d
```

Or equivalently:

```bash
docker run -d -p 8001:8001 \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v /data/swirl:/data \
    -e MODE=standalone \
    -e DB_TYPE=bolt \
    -e DB_ADDRESS=/data \
    --name=swirl \
    cuigh/swirl
```

### Standalone — MongoDB backend

```bash
docker compose -f compose.standalone.yml up -d
```

### Swarm — Docker Stack

```bash
docker stack deploy -c compose.yml swirl
```

### Swarm — Docker Service

```bash
docker service create \
  --name=swirl \
  --publish=8001:8001/tcp \
  --env DB_ADDRESS=mongodb://localhost:27017/swirl \
  --env AGENTS=swirl_manager_agent,swirl_worker_agent \
  --constraint=node.role==manager \
  --mount=type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock \
  cuigh/swirl
```

## Compose stacks in standalone mode

The standalone engine accepts a subset of `docker-compose.yml` v3 and deploys it to a single host:

- **Supported** keys: `services` (image, command, entrypoint, environment, ports, volumes bind/named/tmpfs, networks, restart, labels, user, working_dir, privileged, read_only, cap_add/cap_drop, dns, dns_search, hostname, tty, stdin_open), `networks`, `volumes`.
- **Not supported**: `build`, `healthcheck`, `secrets`, `configs`, `deploy`, `depends_on` ordering.

Containers are labelled with the standard `com.docker.compose.project=<stack-name>`, `com.docker.compose.service=<service>` and `com.swirl.compose.managed=true`. This means stacks created with the plain `docker compose` CLI appear in the Swirl Stacks list as **read-only, unmanaged** (you can see status, but can't Start/Stop/Remove without importing them first).

Deploy lifecycle: **Save** (persist only) / **Deploy** (persist + apply) / **Start** / **Stop** / **Remove** (optionally including volumes).

## Image management

- **Delete** (red): normal `docker rmi`; fails if the image is referenced by a container or has multiple tags.
- **Force delete** (red, confirmation dialog): `Force=true, PruneChildren=true` — removes the image from every repository (untags all) and deletes layers even if referenced.
- **Unused badge**: shown when the image is not referenced by any container (running or stopped).

## Container management

Per-host actions available from the Containers list in both swarm and standalone mode:
- Start, Stop, Restart, Pause/Unpause, Kill
- Rename
- Logs (streamed)
- Exec (WebSocket TTY)
- Stats (one-shot snapshot via the API)
- Delete — disabled while the container is `running` or `paused`.

## Advanced Features

| Label       | Description          | Example                 | Mode  |
|-------------|----------------------|-------------------------|-------|
| swirl.scale | Service auto scaling | `min=1,max=5,cpu=30:50` | Swarm |

## Build

Requirements: Node.js 22+, Go 1.25+.

```sh
cd ui && yarn install && yarn run build && cd ..
go build
```

Go 1.25 is required because Docker SDK v28 transitively pulls
`go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`, whose minimum
Go version is 1.25.

### Docker Build

```bash
docker build -t swirl .
```

The multi-stage Dockerfile uses `node:22-alpine` for the UI build and `golang:1.25-alpine` for the Go build. `.dockerignore` excludes `ui/node_modules`, `ui/dist`, `.git`, `.planning` — keep `ui/dist` ignored: stale dist artefacts in the build context can produce a bundle that dynamic-imports dead chunks.

### Working directory performance (WSL2)

When developing on WSL2 with the repo under `/mnt/c/...`, Docker builds are I/O-bound. A faster workflow is to rsync the repo onto a native Linux path before each build:

```bash
rsync -a --delete \
  --exclude='ui/node_modules' --exclude='ui/dist' \
  --exclude='.git' --exclude='.planning' \
  /mnt/c/GitRepos/swirl/ /opt/DockerData/swirl/

cd /opt/DockerData/swirl
docker build -t swirl:standalone .
docker compose -f docker-compose.standalone-bolt.yml up -d
```

## Architecture

```
┌─────────────────────────────────────────────┐
│  Vue 3 + Naive-UI + TypeScript              │  ui/
│  Mode-aware menu + router guard             │
└────────────────────┬────────────────────────┘
                     │ REST /api/*
┌────────────────────▼────────────────────────┐
│  API Handlers (api/*.go)                    │  struct-tag routing, swarmOnly wrapper
├─────────────────────────────────────────────┤
│  Business Logic (biz/*.go)                  │  DI via auxo container
├─────────────────────────────────────────────┤
│  Docker SDK Wrapper (docker/*.go)           │  d.agent(node) for per-host ops
│  Compose engine (docker/compose/)           │  Swarm stacks + standalone stacks
│  Host manager (docker/host.go)              │  per-host client cache
├─────────────────────────────────────────────┤
│  DAO Layer (dao/)                           │  MongoDB or BoltDB (BSON both)
└─────────────────────────────────────────────┘
```

## License

MIT License.
