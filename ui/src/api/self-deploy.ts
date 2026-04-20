import ajax, { Result } from './ajax'

// Mirrors biz.SelfDeployConfig (v3-simplified: no template,
// no placeholders, no recovery UI). The YAML is read verbatim from the
// compose stack identified by `sourceStackId` and edited through the
// normal compose_stack editor.
export interface SelfDeployConfig {
    enabled: boolean;
    sourceStackId: string;
    autoRollback: boolean;
    deployTimeout: number;
}

export interface SelfDeployStatus {
    phase: string;
    jobId?: string;
    startedAt?: string;
    finishedAt?: string;
    error?: string;
    logTail?: string[];
    // Sidekick introspection (populated by the biz layer at every
    // /status poll via docker inspect + docker logs — lets the UI show
    // sidekick output even when the sidekick crashed before writing any
    // state.json update).
    sidekickContainer?: string;
    sidekickAlive?: boolean;
    sidekickLogs?: string;
    // canReset is true when the on-disk state points at an in-progress
    // phase but the sidekick is missing/exited. The UI surfaces a
    // "Clear stuck lock" button gated on this flag.
    canReset?: boolean;
}

export interface SelfDeployDeployResult {
    jobId: string;
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

    // reset clears a stuck `.lock` + abandoned state.json. Refused with
    // code 1007 when the sidekick is still running.
    reset() {
        return ajax.post<{ reclaimed: boolean }>('/self-deploy/reset')
    }
}

export default new SelfDeployApi
