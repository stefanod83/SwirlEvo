import ajax, { Result } from './ajax'

export interface ComposeStack {
    id?: string;
    hostId: string;
    hostName?: string;
    name: string;
    content: string;
    status?: string;
    errorMessage?: string;
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

export interface ComposeContainerBrief {
    id: string;
    name: string;
    service?: string;
    image: string;
    state: string;
    status: string;
    ports?: { ip: string; privatePort: number; publicPort: number; type: string }[];
    created: string;
}

export interface ComposeStackDetail {
    id?: string;
    hostId: string;
    hostName?: string;
    name: string;
    content?: string;
    reconstructed: boolean;
    status: string;
    managed: boolean;
    services: string[];
    networks: string[];
    volumes: string[];
    containers: ComposeContainerBrief[];
    updatedAt?: string;
}

export interface ComposeStackSearchArgs {
    hostId?: string;
    name?: string;
    pageIndex: number;
    pageSize: number;
}

export interface ActionRef {
    id?: string;
    hostId?: string;
    name?: string;
    removeVolumes?: boolean;
}

export class ComposeStackApi {
    find(id: string) {
        return ajax.get<ComposeStack>('/compose-stack/find', { id })
    }

    findDetail(hostId: string, name: string) {
        return ajax.get<ComposeStackDetail>('/compose-stack/find-detail', { hostId, name })
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

    import_(stack: ComposeStack, redeploy = false, pullImages = false) {
        return ajax.post<{ id: string }>('/compose-stack/import', { ...stack, redeploy, pullImages })
    }

    start(ref: ActionRef) {
        return ajax.post<Result<Object>>('/compose-stack/start', ref)
    }

    stop(ref: ActionRef) {
        return ajax.post<Result<Object>>('/compose-stack/stop', ref)
    }

    remove(ref: ActionRef) {
        return ajax.post<Result<Object>>('/compose-stack/remove', ref)
    }
}

export default new ComposeStackApi
