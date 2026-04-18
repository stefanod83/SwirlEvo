import ajax, { Result } from './ajax'

// Mirrors biz.SelfDeployConfig (v3). No template, no placeholders —
// the YAML is read verbatim from the compose stack identified by
// `sourceStackId` and edited through the normal compose_stack editor.
export interface SelfDeployConfig {
    enabled: boolean;
    sourceStackId: string;
    autoRollback: boolean;
    deployTimeout: number;
    recoveryPort: number;
    recoveryAllow: string[];
}

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

export interface SelfDeployDeployResult {
    jobId: string;
    recoveryUrl: string;
    targetImageTag?: string;
    stackName?: string;
}

// defaultConfig mirrors biz.applyConfigDefaults so the Settings form is
// never empty on first mount (fresh install).
export const defaultConfig: SelfDeployConfig = {
    enabled: false,
    sourceStackId: '',
    autoRollback: true,
    deployTimeout: 300,
    recoveryPort: 8002,
    recoveryAllow: ['127.0.0.1/32'],
}

export class SelfDeployApi {
    loadConfig() {
        return ajax.get<SelfDeployConfig>('/self-deploy/load-config')
    }

    saveConfig(cfg: SelfDeployConfig) {
        return ajax.post<Result<Object>>('/self-deploy/save-config', cfg)
    }

    // Returns 202 with {jobId, recoveryUrl, targetImageTag?, stackName?}.
    deploy() {
        return ajax.post<SelfDeployDeployResult>('/self-deploy/deploy')
    }

    status() {
        return ajax.get<SelfDeployStatus>('/self-deploy/status')
    }
}

export default new SelfDeployApi
