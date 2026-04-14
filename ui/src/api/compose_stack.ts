import ajax, { Result } from './ajax'

export interface ComposeStack {
    id?: string;
    hostId: string;
    hostName?: string;
    name: string;
    content: string;
    status?: string;
    containers?: number;
    running?: number;
    services?: number;
    managed?: boolean;
    createdAt?: number;
    updatedAt?: number;
}

export interface ComposeStackSummary {
    id: string;
    hostId: string;
    hostName?: string;
    name: string;
    status: string;
    containers: number;
    running: number;
    services: number;
    managed: boolean;
    updatedAt?: string;
}

export interface ComposeStackSearchArgs {
    hostId?: string;
    name?: string;
    pageIndex: number;
    pageSize: number;
}

export class ComposeStackApi {
    find(id: string) {
        return ajax.get<ComposeStack>('/compose-stack/find', { id })
    }

    search(args: ComposeStackSearchArgs) {
        return ajax.get<{ items: ComposeStackSummary[]; total: number }>('/compose-stack/search', args)
    }

    save(stack: ComposeStack) {
        return ajax.post<{ id: string }>('/compose-stack/save', stack)
    }

    deploy(stack: ComposeStack, pullImages = false) {
        return ajax.post<{ id: string }>('/compose-stack/deploy', { ...stack, pullImages })
    }

    start(id: string) {
        return ajax.post<Result<Object>>('/compose-stack/start', { id })
    }

    stop(id: string) {
        return ajax.post<Result<Object>>('/compose-stack/stop', { id })
    }

    remove(id: string, removeVolumes = false) {
        return ajax.post<Result<Object>>('/compose-stack/remove', { id, removeVolumes })
    }
}

export default new ComposeStackApi
