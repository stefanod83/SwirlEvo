# SWIRL

**Swirl** is a web management tool for Docker, supporting both **Swarm cluster** and **Standalone host** modes.

> Version 2.0 introduces dual-mode operation: manage Docker Swarm clusters (existing) or standalone Docker hosts (new). All dependencies updated to modern versions.

## Features

* **Dual mode**: Swarm cluster management OR standalone Docker host management
* Swarm components management (services, tasks, stacks, configs, secrets, nodes)
* Image and container management (per-host in standalone mode)
* Compose management with deployment support
* **Standalone host management**: add remote Docker hosts via TCP, TLS, or socket
* Service monitoring based on Prometheus and cadvisor
* Service auto scaling
* LDAP authentication support
* Full permission control based on RBAC model
* Multiple language support (English, Chinese)

## Operating Modes

### Swarm Mode (default)

Traditional Docker Swarm cluster management. Requires Docker Swarm initialized on at least one node. Uses socat agent containers for node-level access.

### Standalone Mode

Manage individual Docker hosts without Swarm. Add remote hosts via the UI with different connection methods:

- **Docker Socket**: Local Unix socket (`unix:///var/run/docker.sock`)
- **TCP**: Remote Docker daemon (`tcp://192.168.1.100:2375`)
- **TCP + TLS**: Secure remote connection with certificates
- **SSH**: SSH tunnel to remote Docker daemon

Set `MODE=standalone` environment variable to activate.

## Configuration

### Environment Variables

| Name               | Default                          | Description                        |
|--------------------|----------------------------------|------------------------------------|
| MODE               | swarm                            | Operating mode: `swarm` or `standalone` |
| DB_TYPE            | mongo                            | Storage engine: `mongo` or `bolt`  |
| DB_ADDRESS         | mongodb://localhost:27017/swirl  | Database connection string         |
| TOKEN_EXPIRY       | 30m                              | JWT token lifetime                 |
| DOCKER_ENDPOINT    | (from env)                       | Docker daemon endpoint             |
| DOCKER_API_VERSION | (auto-negotiated)                | Docker API version (optional)      |
| AGENTS             | (empty)                          | Swarm agent services (swarm mode)  |

### Config File

All options can be set with `config/app.yml`:

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

### Standalone Mode — Docker Compose (simplest)

Single container with BoltDB (no external database needed):

```bash
docker compose -f compose.standalone-bolt.yml up -d
```

With MongoDB:

```bash
docker compose -f compose.standalone.yml up -d
```

### Standalone Mode — Docker Run

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

### Swarm Mode — Docker Stack

```bash
docker stack deploy -c compose.yml swirl
```

### Swarm Mode — Docker Service

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

## Advanced Features

| Label       | Description          | Example                 |
|-------------|----------------------|-------------------------|
| swirl.scale | Service auto scaling | `min=1,max=5,cpu=30:50` |

## Build

Requirements: Node.js 22+, Go 1.22+

```sh
cd ui && npm install && npm run build && cd ..
go build
```

### Docker Build

```bash
docker build -t swirl .
```

## Architecture

```
┌─────────────────────────────────┐
│  Vue 3 + Naive-UI + TypeScript  │  Frontend (ui/)
└────────────┬────────────────────┘
             │ REST API
┌────────────▼────────────────────┐
│  API Handlers (api/*.go)        │  Auth via struct tags
├─────────────────────────────────┤
│  Business Logic (biz/*.go)      │  Interfaces + DI
├─────────────────────────────────┤
│  Docker SDK Wrapper (docker/)   │  Swarm agents + HostManager
├─────────────────────────────────┤
│  DAO Layer (dao/)               │  MongoDB or BoltDB
└─────────────────────────────────┘
```

## License

This product is licensed under the MIT License.
