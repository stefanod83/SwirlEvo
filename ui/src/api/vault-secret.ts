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

// VaultSecretStatus mirrors biz.VaultSecretStatus — health per catalog
// entry retrieved in batch from /vault-secret/statuses.
export interface VaultSecretStatus {
    id: string;
    exists: boolean;
    currentVersion?: number;
    totalVersions?: number;
    error?: string;
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

    write(id: string, data: Record<string, any>, replace = false) {
        return ajax.post<Result<Object>>('/vault-secret/write', { id, data, replace })
    }

    statuses() {
        return ajax.get<Record<string, VaultSecretStatus>>('/vault-secret/statuses')
    }
}

export default new VaultSecretApi
