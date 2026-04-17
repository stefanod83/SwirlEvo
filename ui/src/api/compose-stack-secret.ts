import ajax, { Result } from './ajax'

// ComposeStackSecretBinding links a compose stack to a VaultSecret catalog
// entry and describes how the resolved value should be injected into the
// target service container at deploy time. The binding never carries the
// secret value — only metadata.
export interface ComposeStackSecretBinding {
    id: string;
    stackId: string;
    vaultSecretId: string;
    field?: string;
    service?: string;
    targetType: 'file' | 'env';
    targetPath?: string;
    envName?: string;
    uid?: number;
    gid?: number;
    mode?: string;
    storageMode?: 'tmpfs' | 'volume' | 'init';
    deployedHash?: string;
    deployedAt?: number;
    createdAt?: number;
    updatedAt?: number;
    createdBy?: { id: string; name: string };
    updatedBy?: { id: string; name: string };
}

export interface ComposeStackSecretSaveResult {
    id?: string;
}

// Mirror of biz.DriftStatus — see biz/compose_stack_secret.go.
// State is one of: ok | drifted | missing | error | unknown.
export interface ComposeStackSecretDrift {
    bindingId: string;
    state: 'ok' | 'drifted' | 'missing' | 'error' | 'unknown';
    currentHash?: string;
    deployedHash?: string;
    message?: string;
}

export class ComposeStackSecretApi {
    list(stackId: string) {
        return ajax.get<ComposeStackSecretBinding[]>('/compose-stack-secret/list', { stackId })
    }

    find(id: string) {
        return ajax.get<ComposeStackSecretBinding>('/compose-stack-secret/find', { id })
    }

    save(binding: Partial<ComposeStackSecretBinding>) {
        return ajax.post<ComposeStackSecretSaveResult>('/compose-stack-secret/save', binding)
    }

    delete(id: string) {
        return ajax.post<Result<Object>>('/compose-stack-secret/delete', { id })
    }

    drift(stackId: string) {
        return ajax.get<ComposeStackSecretDrift[]>('/compose-stack-secret/drift', { stackId })
    }
}

export default new ComposeStackSecretApi
