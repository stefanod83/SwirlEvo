import ajax, { Result } from './ajax'

export interface ComposeStack {
    id?: string;
    hostId: string;
    hostName?: string;
    name: string;
    content: string;
    envFile?: string;
    status?: string;
    errorMessage?: string;
    // lastWarnings carries non-fatal observations from the most recent
    // deploy (e.g. compose fields silently ignored in standalone mode).
    lastWarnings?: string[];
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
    // force overrides the "volumes contain data" preservation check when
    // set. The UI obtains it by showing a second confirmation dialog
    // with the list returned by the first (unforced) attempt.
    force?: boolean;
}

// RemoveResponse is returned by /compose-stack/remove. When the backend
// refused the removal because non-empty volumes would be wiped, it sends
// a success payload (code=0) carrying `volumesContainData=true` + the
// list of affected volume names — the UI then asks for confirmation.
export interface RemoveResponse {
    volumesContainData?: boolean;
    volumes?: string[];
}

// Addon discovery types — consumed by the stack editor wizard tabs.
// Fields are optional because any add-on may be missing from the host.
// DiscoveryValue carries a provenance badge ("docker" from live inspect,
// "file" from an uploaded config file stored in Host.AddonConfigExtract).
export interface DiscoveryValue {
    name: string;
    origin: string;
}

export interface TraefikAddon {
    containerName?: string;
    image?: string;
    version?: string;
    entryPoints: DiscoveryValue[];
    certResolvers: DiscoveryValue[];
    middlewares: DiscoveryValue[];
    networks: DiscoveryValue[];
    dockerNetwork?: string;
    sablierPlugin?: boolean;
}

export interface SablierAddon {
    containerName?: string;
    image?: string;
    url?: string;
    networks?: string[];
}

export interface WatchtowerAddon {
    containerName?: string;
    image?: string;
    labelEnable?: boolean;
    includeStopped?: boolean;
    pollInterval?: number;
}

export interface BackupAddon {
    containerName?: string;
    image?: string;
    schedule?: string;
    retentionEnv?: string;
    targetDir?: string;
}

export interface HostAddons {
    traefik?: TraefikAddon;
    sablier?: SablierAddon;
    watchtower?: WatchtowerAddon;
    backup?: BackupAddon;
}

// AddonsConfig mirrors the Go DTO — one wizard-state map per addon, keyed
// by service name. Missing maps / empty objects are treated as "no wizard
// state", so the backend leaves those services untouched.
export interface TraefikServiceCfg {
    enabled: boolean;
    router?: string;
    ruleType?: 'Host' | 'PathPrefix' | 'Host+PathPrefix' | '';
    domain?: string;
    path?: string;
    entrypoint?: string;
    port?: number;
    tls?: boolean;
    certResolver?: string;
    middlewares?: string[];
}

export interface ResourcesServiceCfg {
    cpusLimit?: string;
    cpusReservation?: string;
    memoryLimit?: string;
    memoryReservation?: string;
}

export interface AddonsConfig {
    traefik?: Record<string, TraefikServiceCfg>;
    resources?: Record<string, ResourcesServiceCfg>;
}

// ComposeStackVersion is a point-in-time snapshot of the stack content.
// List responses omit Content/EnvFile to keep the payload small; fetch
// a single version with versionGet() when rendering a diff.
export interface ComposeStackVersion {
    id: string;
    stackId: string;
    revision: number;
    content?: string;
    envFile?: string;
    reason: string;
    createdAt?: string;
    createdBy?: { id?: string; name?: string };
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

    save(stack: ComposeStack, addonsConfig?: AddonsConfig) {
        const payload = addonsConfig ? { ...stack, addonsConfig } : stack
        return ajax.post<{ id: string }>('/compose-stack/save', payload)
    }

    deploy(stack: ComposeStack, pullImages = false, addonsConfig?: AddonsConfig) {
        return ajax.post<{ id: string }>('/compose-stack/deploy',
            { ...stack, pullImages, addonsConfig })
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
        return ajax.post<RemoveResponse>('/compose-stack/remove', ref)
    }

    migrate(id: string, targetHostId: string, redeploy: boolean) {
        return ajax.post<Result<Object>>('/compose-stack/migrate', { id, targetHostId, redeploy })
    }

    hostAddons(hostId: string) {
        return ajax.get<HostAddons>('/compose-stack/host-addons', { hostId })
    }

    versions(stackId: string) {
        return ajax.get<{ items: ComposeStackVersion[] }>('/compose-stack/versions', { stackId })
    }

    versionGet(id: string) {
        return ajax.get<ComposeStackVersion>('/compose-stack/version-get', { id })
    }

    versionRestore(stackId: string, versionId: string) {
        return ajax.post<Result<Object>>('/compose-stack/version-restore', { stackId, versionId })
    }

    parseAddons(content: string) {
        return ajax.post<AddonsConfig>('/compose-stack/parse-addons', { content })
    }
}

export default new ComposeStackApi
