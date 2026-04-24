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
    networks?: {
        name: string;
        ip?: string;
        ipv6?: string;
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
    // Resources reflects the runtime limits/reservations configured on
    // the container via HostConfig.Resources. Populated only on the
    // detail view (summary doesn't carry it). Undefined when the
    // container has no explicit limits.
    resources?: ContainerResources;
}

export interface ContainerResources {
    // CPUs limit expressed in full CPU units (1.0 = one core). Derived
    // from HostConfig.NanoCPUs / 1e9.
    cpus?: number;
    // CPUShares is the relative CPU weight under contention (default
    // 1024 = "one full CPU share"). Populated when the standalone
    // engine approximates deploy.resources.reservations.cpus — no hard
    // CPU floor exists in docker-run.
    cpuShares?: number;
    // Memory limit in bytes (0 = no limit).
    memory?: number;
    // Soft memory limit (--memory-reservation) in bytes.
    memoryReservation?: number;
    // Swap limit (-1 = unlimited, 0 = inherit Memory, >0 = explicit).
    memorySwap?: number;
    // PIDs limit (0 = unlimited).
    pidsLimit?: number;
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

    delete(node: string, id: string, name: string, volumes: boolean = false) {
        return ajax.post<Result<Object>>('/container/delete', { node, id, name, volumes })
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
