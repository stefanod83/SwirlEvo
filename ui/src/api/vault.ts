import ajax from './ajax'

export interface VaultTestResult {
    ok: boolean;
    stage?: 'health' | 'auth' | 'ok';
    error?: string;
    sealed?: boolean;
    initialized?: boolean;
    version?: string;
}

export class VaultApi {
    // ajax.post() already unwraps the outer envelope and returns
    // Result<VaultTestResult>, so the response body is `r.data` (NOT
    // `r.data.data` — that double-access was the source of the silent
    // "Vault connection failed" message).
    test() {
        return ajax.post<VaultTestResult>('/vault/test', {})
    }
}

export default new VaultApi
