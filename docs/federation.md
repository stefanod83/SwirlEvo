# Federation (multi-cluster portal)

A Swirl instance in `MODE=standalone` can act as a portal that
manages multiple Docker hosts:

- **Local standalone hosts** (`unix://`, `tcp://`, `tcp+tls://`, `ssh://`)
- **Remote Swarm clusters** via federation to another Swirl instance
  deployed in `MODE=swarm` inside the cluster

This page covers the federation flow. For per-host standalone
management see the Hosts page in the UI.

---

## Why federation and not direct socket?

Connecting the portal directly to a Swarm manager's Docker socket
would require exposing that socket (root-equivalent) to the portal's
network. Federation keeps the Docker socket inside the cluster —
the portal only sees a plain HTTPS endpoint authenticated by a
long-lived bearer token.

| Aspect | Federation via Swirl | Direct socket |
|---|---|---|
| Exposed surface | HTTPS REST | Docker socket (root) |
| Authentication | bearer token, rotatable, revocable | TLS client cert |
| Audit | recorded on both portal + target | only on portal |
| Per-node operations | socat-agent internal to the cluster | requires per-node socket access from outside |
| Firewall | 443 (already open) | 2376 + per-node |

Direct-socket for Swarm managers is **intentionally unsupported** —
a probe at Save time rejects `tcp+tls://` endpoints pointing at a
manager, with a message nudging the operator toward federation.

---

## Setting up federation

### 1. Deploy Swirl inside the cluster (`MODE=swarm`)

Deploy a Swirl service on the manager node. Typical compose:

```yaml
services:
  swirl:
    image: registry.example.com/swirl:latest
    environment:
      - MODE=swarm
      - DB_TYPE=mongo
      - DB_ADDRESS=mongodb://mongo:27017/swirl
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    deploy:
      placement:
        constraints: [node.role == manager]
    networks:
      - traefik-net
    labels:
      - traefik.enable=true
      - traefik.http.routers.swirl.rule=Host(`swirl-swarm.internal`)
      - traefik.http.services.swirl.loadbalancer.server.port=8001
```

Expose this Swirl via Traefik (or your ingress) with TLS. It should
be reachable from the portal Swirl at `https://swirl-swarm.internal/`.

### 2. Generate a federation peer token on the target

On the swarm-side Swirl, an operator with `federation.admin` role
mints a peer:

```bash
curl -X POST https://swirl-swarm.internal/api/federation/peers \
  -H "Authorization: Bearer $SWARM_SWIRL_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "portal-1", "ttlDays": 90}'
```

Response (token shown **once**, copy immediately):

```json
{
  "code": 0,
  "data": {
    "id": "5f2b...",
    "name": "portal-1",
    "loginName": "federation-peer-portal-1",
    "token": "a1b2c3...64hexchars",
    "expiresAt": "2026-07-20T10:00:00Z"
  }
}
```

A UI panel for token management lives in Settings → Federation on
the swarm-side Swirl (same endpoints, same flow). The Settings panel
is **gated on `MODE=swarm`** — it appears only on Swirl instances
running inside a cluster, never on the portal. Rationale: the
standalone portal is a token **consumer** (it receives a peer token
pasted into Hosts → Add), not a minter. The panel is also hidden
unless the logged-in user holds the `federation.admin` permission.

### 3. Register the cluster on the portal

On the portal Swirl (`MODE=standalone`), navigate to Hosts → Add.
Fill in:

- **Name**: e.g. `prod-cluster`
- **Endpoint**: `https://swirl-swarm.internal/` — the probe on Save
  detects the HTTPS scheme and classifies the host as
  `swarm_via_swirl`, enabling the federation fields.
- **Federation peer token**: paste the token copied in step 2
- **Auto-refresh token**: enable to let the portal periodically
  rotate the token before it expires (requires `federation.admin`
  on the target Swirl)
- **Color**: optional, for the visual marker under the header

Save. The portal calls `GET /api/federation/capabilities` against
the target, and if the handshake succeeds the host appears in the
dropdown.

### 4. Use the cluster

Select the cluster in the dropdown. The sidebar now includes the
**Swarm** group (Services, Tasks, Stacks, Configs, Secrets, Nodes,
Networks) alongside Local. Every API call is proxied by the portal
to the target Swirl via:

```
POST /api/service/search?node=<host-id>
  ↓
POST https://swirl-swarm.internal/api/service/search
  Authorization: Bearer <peer-token>
  X-Swirl-Originating-User: <portal-user-name>
```

The target Swirl validates the peer token, executes the action
against its own docker socket + socat-agent, and returns the
response. The portal streams it back to the operator's browser.

WebSocket connections (exec, logs, stats) are forwarded by the same
middleware via a TCP tunnel — behaviour indistinguishable from
direct access to the portal.

---

## Audit

Every proxied request carries an `X-Swirl-Originating-User` header
identifying the human operator on the portal side. The target
Swirl persists this in `dao.Event.OriginatingUser` so its own audit
log shows:

```
Username        : federation-peer-portal-1
OriginatingUser : alice
```

(Biz-layer migration to populate this field is wave 4; infrastructure
is ready in the DAO + the security filter.)

---

## Token lifecycle

- **TTL**: configurable per-peer at creation (`ttlDays`). Default
  90 days. Zero or negative means no expiry (~100 years).
- **Soft-expiry**: past the expiry date the token keeps working.
  The UI shows a warning banner so the operator rotates manually.
- **Rotation**: admin calls `POST /api/federation/peers/rotate`
  with the peer ID. Returns the new token (shown once). The old
  token is invalidated immediately.
- **Revocation**: `POST /api/federation/peers/revoke` deletes the
  peer user entirely. Future requests with that token return 401.

---

## Security considerations

- The federation token is a long-lived bearer. Protect it at rest
  on the portal host: Swirl masks it in UI responses with the
  standard secret-mask placeholder; the backup archive also masks
  it (like every `secret_id`/`client_secret` field).
- The `X-Swirl-Originating-User` header is **audit-only** — the
  target Swirl does not honour it for authorization. The only
  identity that matters is the peer token.
- Swarm worker endpoints are rejected at Save time. The error
  lists the cluster's manager addresses so the operator can switch
  to the correct target without digging through Docker Info output.
- The `local` host (auto-registered at boot in standalone mode,
  pointing at `unix:///var/run/docker.sock`) is immutable: its
  `Endpoint`, `AuthMethod`, `Type`, `SwirlURL` and `Immutable`
  flags are frozen and cannot be changed via the API. Delete is
  refused with 403 (`ErrHostImmutable`). **Cosmetic** updates —
  `Name` and `Color` — ARE accepted, so the UI's color picker
  still works on the local host; the biz layer applies them onto
  the persisted record while preserving every other field.

---

## Limitations (v1)

- **Stack.HostID not persisted yet**: multi-cluster Swarm stack
  authoring (same stack name on cluster A and cluster B) collides
  on the DB primary key. Single-cluster multi-portal works. Full
  multi-cluster support lands in a follow-up wave.
- **No auto-refresh loop yet**: `tokenAutoRefresh` is a stored
  preference; the background rotation ticker arrives in the
  follow-up wave. For now, rotate manually via the UI button.
- **No UI banner for expired tokens yet**: status appears in the
  Host Edit form. Global banner in a follow-up.
- **Audit enrichment**: `OriginatingUser` is captured by the
  security middleware and available in DAO; biz-layer event
  emitters will be migrated to consume it in a follow-up.

All of these are **additive** follow-ups — the federation core
(handshake, token auth, reverse proxy, WebSocket forwarding) is
complete and functional.
