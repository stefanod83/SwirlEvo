import ajax, { Result } from './ajax'

export interface Registry {
    id: string;
    name: string;
    url: string;
    username: string;
    password: string;
    // Disables TLS verification for Swirl's own HTTP calls to the
    // registry (catalog/tags). Enable only for registries with
    // self-signed certs. Docker daemon push still relies on
    // `insecure-registries` in daemon.json.
    skipTlsVerify?: boolean;
    createdAt: number;
    updatedAt: number;
    createdBy: {
        id: string;
        name: string;
    };
    updatedBy: {
        id: string;
        name: string;
    };
}

export interface RegistryBrowseResult {
    repos: string[];
    next?: string;
}

export class RegistryApi {
    find(id: string) {
        return ajax.get<Registry>('/registry/find', { id })
    }

    search() {
        return ajax.get<Registry[]>('/registry/search')
    }

    save(registry: Registry) {
        return ajax.post<Result<Object>>('/registry/save', registry)
    }

    delete(id: string) {
        return ajax.post<Result<Object>>('/registry/delete',  { id })
    }

    browse(id: string, pageSize = 100, last = '') {
        return ajax.get<RegistryBrowseResult>('/registry/browse', { id, pageSize, last })
    }

    tags(id: string, repo: string) {
        return ajax.get<string[]>('/registry/tags', { id, repo })
    }
}

export default new RegistryApi
