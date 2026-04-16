import ajax, { Result } from './ajax'

export interface Backup {
    id: string;
    name: string;
    source: string;
    size: number;
    checksum: string;
    path: string;
    includes?: string[];
    stats?: { [key: string]: number };
    // KeyFingerprint of the master key the archive was encrypted with.
    // Empty for backups created before this field was introduced.
    keyFingerprint?: string;
    // KeyStatus is computed server-side at read time. The UI should treat
    // it as advisory: 'compatible' / 'incompatible' / 'unverified' / 'missing' / 'unknown'.
    keyStatus?: 'compatible' | 'incompatible' | 'unverified' | 'missing' | 'unknown';
    verifiedAt?: string | null;
    createdAt: string;
    createdBy?: {
        id: string;
        name: string;
    };
}

// BackupKeyStatusSummary mirrors biz.BackupKeyStatusSummary — it's the
// aggregate result of /backup/key-status.
export interface BackupKeyStatusSummary {
    total: number;
    compatible: number;
    incompatible: number;
    unverified: number;
    missing: number;
    keyMissing: boolean;
    fingerprint?: string;
}

export interface BackupSchedule {
    id: 'daily' | 'weekly' | 'monthly';
    enabled: boolean;
    dayConfig: string;
    time: string;
    retention: number;
    lastRunAt?: string | null;
    createdAt?: string;
    updatedAt?: string;
}

export interface BackupManifest {
    version: string;
    exportedAt: string;
    swirlVersion?: string;
    stats: { [key: string]: number };
}

export interface BackupStatus {
    keyConfigured: boolean;
    // 'env' | 'cache' | 'vault' | '' (empty when no key was found)
    keySource?: string;
    // Populated when a Vault provider lookup fails — surfaced verbatim in
    // the UI so the operator sees the real reason (wrong path / wrong
    // field / Vault unreachable) instead of a silent "not configured".
    keyError?: string;
}

export class BackupApi {
    status() {
        return ajax.get<BackupStatus>('/backup/status')
    }

    search() {
        return ajax.get<Backup[]>('/backup/search')
    }

    find(id: string) {
        return ajax.get<Backup>('/backup/find', { id })
    }

    create(source = 'manual') {
        return ajax.post<Backup>('/backup/create', { source })
    }

    delete(id: string) {
        return ajax.post<Result<Object>>('/backup/delete', { id })
    }

    // Returns raw archive bytes as a blob for file-save.
    async download(id: string, mode: 'raw' | 'portable' = 'raw', password = ''): Promise<Blob> {
        const r = await ajax.request<Blob>({
            url: '/backup/download',
            method: 'post',
            data: { id, mode, password },
            responseType: 'blob',
            timeout: 5 * 60 * 1000,
        })
        return r as unknown as Blob
    }

    restore(id: string, components: string[]) {
        return ajax.request<{ [key: string]: number }>({
            url: '/backup/restore',
            method: 'post',
            data: { id, components },
            headers: { 'Content-Type': 'application/json' },
            timeout: 5 * 60 * 1000,
        })
    }

    preview(file: File, password = '') {
        const form = new FormData()
        form.append('content', file)
        form.append('password', password)
        return ajax.request<BackupManifest>({
            url: '/backup/preview',
            method: 'post',
            data: form,
            timeout: 2 * 60 * 1000,
        })
    }

    upload(file: File, password: string, components: string[]) {
        const form = new FormData()
        form.append('content', file)
        form.append('password', password)
        for (const c of components) form.append('components', c)
        return ajax.request<{ [key: string]: number }>({
            url: '/backup/upload',
            method: 'post',
            data: form,
            timeout: 5 * 60 * 1000,
        })
    }

    schedules() {
        return ajax.get<BackupSchedule[]>('/backup/schedules')
    }

    saveSchedule(schedule: BackupSchedule) {
        return ajax.post<Result<Object>>('/backup/schedule/save', schedule)
    }

    deleteSchedule(id: string) {
        return ajax.post<Result<Object>>('/backup/schedule/delete', { id })
    }

    keyStatus() {
        return ajax.get<BackupKeyStatusSummary>('/backup/key-status')
    }

    verify(id: string) {
        return ajax.post<Backup>('/backup/verify', { id })
    }

    recover(id: string, oldPassphrase: string) {
        return ajax.post<Backup>('/backup/recover', { id, oldPassphrase })
    }
}

export default new BackupApi()
