import ajax, { Result } from './ajax'

// Mirrors biz.SelfDeployPlaceholders (misc.SelfDeployPlaceholders
// under the hood). The UI stores CIDR and traefik-label lists as plain
// string arrays and only converts to/from the newline-per-item
// textareas at the binding boundary.
export interface SelfDeployPlaceholders {
    imageTag: string;
    exposePort: number;
    recoveryPort: number;
    recoveryAllow: string[];
    traefikLabels: string[];
    volumeData: string;
    networkName: string;
    containerName: string;
    extraEnv: Record<string, string>;
}

// Mirrors biz.SelfDeployConfig.
export interface SelfDeployConfig {
    enabled: boolean;
    template: string;
    placeholders: SelfDeployPlaceholders;
    autoRollback: boolean;
    deployTimeout: number;
}

// Mirrors biz.SelfDeployStatus + the recoveryActive flag added by the
// API layer. `startedAt` / `finishedAt` are intentionally optional —
// the idle snapshot carries neither.
export interface SelfDeployStatus {
    phase: string;
    jobId?: string;
    startedAt?: string;
    finishedAt?: string;
    error?: string;
    logTail?: string[];
    recoveryActive: boolean;
    recoveryUrl?: string;
}

export interface SelfDeployPreviewResult {
    yaml: string;
}

export interface SelfDeployDeployResult {
    jobId: string;
    recoveryUrl: string;
    targetImageTag: string;
}

// DefaultPlaceholders mirrors biz.DefaultPlaceholders so the form can
// be populated with sensible values even when the backend has never
// returned a config yet (fresh install).
export const defaultPlaceholders: SelfDeployPlaceholders = {
    imageTag: 'cuigh/swirl:latest',
    exposePort: 8001,
    recoveryPort: 8002,
    recoveryAllow: ['127.0.0.1/32'],
    traefikLabels: [],
    volumeData: 'swirl_data',
    networkName: 'swirl_net',
    containerName: 'swirl',
    extraEnv: {},
}

export const defaultConfig: SelfDeployConfig = {
    enabled: false,
    template: '',
    placeholders: defaultPlaceholders,
    autoRollback: true,
    deployTimeout: 300,
}

export class SelfDeployApi {
    loadConfig() {
        return ajax.get<SelfDeployConfig>('/self-deploy/load-config')
    }

    saveConfig(cfg: SelfDeployConfig) {
        return ajax.post<Result<Object>>('/self-deploy/save-config', cfg)
    }

    // `override` is optional — when omitted, the backend renders with
    // the persisted config. Passing an override lets the UI preview a
    // set of placeholders before the user hits Save.
    preview(override?: Partial<SelfDeployPlaceholders>) {
        const body = override ? { placeholders: override } : {}
        return ajax.post<SelfDeployPreviewResult>('/self-deploy/preview', body)
    }

    // Returns 202 with {jobId, recoveryUrl, targetImageTag}. The UI
    // should start a health-check poll after a short grace period
    // because the primary is about to be stopped by the sidekick.
    deploy() {
        return ajax.post<SelfDeployDeployResult>('/self-deploy/deploy')
    }

    status() {
        return ajax.get<SelfDeployStatus>('/self-deploy/status')
    }
}

export default new SelfDeployApi
