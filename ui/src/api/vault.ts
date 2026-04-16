import ajax, { Result } from './ajax'

export interface VaultTestResult {
    ok: boolean;
    stage?: 'health' | 'auth' | 'ok';
    error?: string;
    sealed?: boolean;
    initialized?: boolean;
    version?: string;
}

export class VaultApi {
    test() {
        return ajax.post<Result<VaultTestResult>>('/vault/test', {})
    }
}

export default new VaultApi
