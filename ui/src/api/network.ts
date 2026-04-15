import ajax, { Result } from './ajax'

export interface Network {
    id: string;
    name: string;
    created: string;
    scope: string;
    driver: string;
    internal: boolean;
    attachable: boolean;
    ingress: boolean;
    ipv6: boolean;
    ipam: {
        driver: string;
        options: [];
        config: {
            subnet: string;
            gateway: string;
            range: string;
        }[],
    }
    options?: {
        name: string;
        value: string;
    }[];
    labels?: {
        name: string;
        value: string;
    }[];
    containers?: {
        id: string;
        name: string;
        mac: string;
        ipv4: string;
        ipv6: string;
    }[];
}

export interface FindResult {
    network: Network;
    raw: string;
}

export interface NetworkTopologyNode {
    id: string;
    type: 'host' | 'network' | 'container';
    label: string;
    meta?: Record<string, any>;
    flags?: string[];
}

export interface NetworkTopologyEdge {
    source: string;
    target: string;
    type: 'host-network' | 'network-container' | 'host-container';
    label?: string;
}

export interface NetworkTopology {
    hostId: string;
    nodes: NetworkTopologyNode[];
    edges: NetworkTopologyEdge[];
}

export class NetworkApi {
    find(name: string, node = '') {
        return ajax.get<FindResult>('/network/find', { name, node })
    }

    search(node = '') {
        return ajax.get<Network[]>('/network/search', { node })
    }

    save(network: Network, node = '') {
        return ajax.post<Result<Object>>('/network/save', { ...network, node })
    }

    delete(id: string, name: string, node = '') {
        return ajax.post<Result<Object>>('/network/delete', { id, name, node })
    }

    disconnect(networkId: string, networkName: string, container: string) {
        return ajax.post<Result<Object>>('/network/disconnect', { networkId, networkName, container })
    }

    topology(hostId: string) {
        return ajax.get<NetworkTopology>('/network/topology', { hostId })
    }
}

export default new NetworkApi
