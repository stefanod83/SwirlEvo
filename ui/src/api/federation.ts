import ajax, { Result } from './ajax'

// FederationCapabilities is the handshake struct the swarm Swirl
// returns on GET /api/federation/capabilities. Consumed by the portal
// at Host save time to verify connectivity + API compatibility.
export interface FederationCapabilities {
    apiVersion: number
    mode: 'swarm' | 'standalone'
    version: string
    features: string[]
    nodes: number
    peerName: string
}

// FederationPeer is a row in the Federation Settings panel listing
// the peer accounts registered on THIS Swirl instance. Never
// includes the raw token — that's returned once at creation.
export interface FederationPeer {
    id: string
    name: string
    loginName: string
    expiresAt: number
    createdAt: number
    expired: boolean
}

// FederationPeerResult is the response of create / rotate: the
// plaintext token is included ONCE and must be copied immediately
// — retrieving it again requires rotation.
export interface FederationPeerResult {
    id: string
    name: string
    loginName: string
    token: string
    expiresAt: number
    createdAt: number
}

export class FederationApi {
    capabilities() {
        return ajax.get<FederationCapabilities>('/federation/capabilities')
    }

    listPeers() {
        return ajax.get<{ items: FederationPeer[] }>('/federation/peers')
    }

    createPeer(name: string, ttlDays: number) {
        return ajax.post<FederationPeerResult>('/federation/peers', { name, ttlDays })
    }

    rotatePeer(id: string, ttlDays: number) {
        return ajax.post<FederationPeerResult>('/federation/peers/rotate', { id, ttlDays })
    }

    revokePeer(id: string) {
        return ajax.post<Result<Object>>('/federation/peers/revoke', { id })
    }
}

export default new FederationApi
