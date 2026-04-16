import ajax, { Result } from './ajax'

export interface Container {
    id: string;
    pid: number;
    name: string;
    image: string;
    command: string;
    createdAt: string;
    startedAt: string;
    sizeRw: number;
    sizeRootFs: number;
    state: string;
    status: string;
    networkMode: string;
    ports?: {
        ip: string;
        privatePort: number;
        publicPort: number;
        type: string;
    }[];
    mounts?: {
        type: string;
        name: string;
        source: string;
        destination: string;
        driver: string;
        mode: string;
        rw: boolean;
        propagation: string;
    }[];
    labels?: {
        name: string;
        value: string;
    }[];
}

export interface SearchArgs {
    node?: string;
    name?: string;
    status?: string;
    project?: string;
    pageIndex: number;
    pageSize: number;
}

export interface SearchResult {
    items: Container[];
    total: number;
}

export interface FindResult {
    container: Container;
    raw: string;
}

export interface FetchLogsArgs {
    node: string;
    id: string;
    lines: number;
    timestamps: boolean;
}

export class ContainerApi {
    find(node: string, id: string) {
        return ajax.get<FindResult>('/container/find', { node, id })
    }

    search(args: SearchArgs) {
        return ajax.get<SearchResult>('/container/search', args)
    }

    delete(node: string, id: string, name: string) {
        return ajax.post<Result<Object>>('/container/delete', { node, id, name })
    }

    start(node: string, id: string, name: string) {
        return ajax.post<Result<Object>>('/container/start', { node, id, name })
    }
    stop(node: string, id: string, name: string, timeout = 0) {
        return ajax.post<Result<Object>>('/container/stop', { node, id, name, timeout })
    }
    restart(node: string, id: string, name: string, timeout = 0) {
        return ajax.post<Result<Object>>('/container/restart', { node, id, name, timeout })
    }
    kill(node: string, id: string, name: string, signal = '') {
        return ajax.post<Result<Object>>('/container/kill', { node, id, name, signal })
    }
    pause(node: string, id: string, name: string) {
        return ajax.post<Result<Object>>('/container/pause', { node, id, name })
    }
    unpause(node: string, id: string, name: string) {
        return ajax.post<Result<Object>>('/container/unpause', { node, id, name })
    }
    rename(node: string, id: string, name: string, newName: string) {
        return ajax.post<Result<Object>>('/container/rename', { node, id, name, newName })
    }
    stats(node: string, id: string) {
        return ajax.get<any>('/container/stats', { node, id })
    }

    fetchLogs(args: FetchLogsArgs) {
        return ajax.get<{
            stdout: string;
            stderr: string;
        }>('/container/fetch-logs', args)
    }

    prune(node: string) {
        return ajax.post<{
            count: number;
            size: number;
        }>('/container/prune', { node })
    }
}

export default new ContainerApi
