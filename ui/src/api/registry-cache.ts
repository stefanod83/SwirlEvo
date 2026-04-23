import ajax from './ajax'

// Response of POST /api/registry-cache/gen-ca. The private key is
// returned ONCE and never stored by Swirl — the operator is responsible
// for saving it offline, configuring it on the mirror, and losing it
// means generating a fresh CA next time.
export interface GenCAResult {
    certPEM: string;
    keyPEM: string;
}

// Response of POST /api/registry-cache/ping. Mirror the Go
// biz.RegistryCachePingResult shape. 200 and 401 from /v2/ both count
// as `ok=true` — the mirror is alive + TLS is fine; auth is a
// registry-specific concern the browse feature handles elsewhere.
export interface PingResult {
    ok: boolean;
    status?: number;
    error?: string;
    latencyMs?: number;
    mirrorUrl?: string;
}

export class RegistryCacheApi {
    /**
     * Generate a self-signed CA certificate + ECDSA P-256 private key.
     * The returned cert is what operators paste into Setting.registry_cache
     * .ca_cert_pem (after Save, it gets distributed to hosts via bootstrap
     * script). The key is given to the operator ONCE — Swirl never persists
     * it.
     *
     * @param commonName optional Common Name for the CA (defaults to
     *                   "Swirl Registry Cache CA" server-side).
     */
    genCA(commonName?: string) {
        return ajax.post<GenCAResult>('/registry-cache/gen-ca', {
            commonName: commonName || '',
        })
    }

    /**
     * Probe the configured mirror URL. Returns status + latency so the
     * Settings tab can show a live badge and the Host edit panel can
     * reassure operators that the copy-paste bootstrap will land on a
     * reachable mirror.
     */
    ping() {
        return ajax.post<PingResult>('/registry-cache/ping')
    }
}

export default new RegistryCacheApi
