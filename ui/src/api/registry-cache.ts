import ajax from './ajax'

// Response of POST /api/registry-cache/gen-ca. The private key is
// returned ONCE and never stored by Swirl — the operator is responsible
// for saving it offline, configuring it on the mirror, and losing it
// means generating a fresh CA next time.
export interface GenCAResult {
    certPEM: string;
    keyPEM: string;
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
}

export default new RegistryCacheApi
