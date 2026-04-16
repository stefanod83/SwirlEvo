import ajax, { Result } from './ajax'

// VaultSecret is the UI projection of dao.VaultSecret. It only holds a
// pointer to a KVv2 entry — the value itself lives inside Vault and is
// never returned by the Swirl API.
export interface VaultSecret {
    id: string;
    name: string;
    desc?: string;
    path: string;
    field?: string;
    labels?: Record<string, string>;
    createdAt: number;
    updatedAt: number;
    createdBy?: { id: string; name: string };
    updatedBy?: { id: string; name: string };
}

export interface VaultSecretSearchArgs {
    name?: string;
    pageIndex?: number;
    pageSize?: number;
}

export interface VaultSecretSearchResult {
    items: VaultSecret[];
    total: number;
}

export interface VaultSecretPreview {
    exists: boolean;
    fields: string[];
}

export interface VaultSecretSaveResult {
    id?: string;
}

export class VaultSecretApi {
    search(args: VaultSecretSearchArgs) {
        return ajax.get<VaultSecretSearchResult>('/vault-secret/search', args)
    }

    list() {
        return ajax.get<VaultSecret[]>('/vault-secret/list')
    }

    find(id: string) {
        return ajax.get<VaultSecret>('/vault-secret/find', { id })
    }

    save(secret: Partial<VaultSecret>) {
        return ajax.post<VaultSecretSaveResult>('/vault-secret/save', secret)
    }

    delete(id: string) {
        return ajax.post<Result<Object>>('/vault-secret/delete', { id })
    }

    preview(id: string) {
        return ajax.get<VaultSecretPreview>('/vault-secret/preview', { id })
    }
}

export default new VaultSecretApi
