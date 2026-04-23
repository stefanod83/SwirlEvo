import ajax, { Result } from './ajax'

// Placeholder the backend returns when a sensitive field
// (vault.token, vault.secret_id, keycloak.client_secret) has a value
// stored. Saving the form with this value (or an empty string)
// instructs the backend to preserve the existing secret. Must match
// biz/setting.go::SettingSecretMask byte-for-byte.
export const SETTING_SECRET_MASK = '••••••••'

export interface Setting {
    ldap: LdapSetting;
    keycloak: KeycloakSetting;
    metric: MetricSetting;
    deploy: DeployOptions;
    vault: VaultSetting;
    backup: BackupStorageSetting;
    registry_cache: RegistryCacheSetting;
}

export interface RegistryCacheSetting {
    enabled: boolean;
    // When set, Hostname/Port/Username/Password/CA* are AUTHORITATIVELY
    // read from the referenced Registry at every Save; the inline
    // values below are overwritten by the backend overlay. Empty =
    // inline mode (legacy).
    registry_id?: string;
    hostname: string;
    port: number;
    ca_cert_pem: string;
    ca_fingerprint: string;
    username: string;
    password: string;
    // When true (default) the deploy-time rewriter lays images at
    // `<mirror>/<registry-domain>/<repo>:<tag>` (multi-upstream
    // Harbor/Nexus layout). When false it strips the domain:
    // `<mirror>/<repo>:<tag>` (single-upstream mirror convention).
    use_upstream_prefix: boolean;
    rewrite_mode: 'off' | 'per-host' | 'always';
    preserve_digests: boolean;
}

export interface BackupStorageSetting {
    // 'fs' (default) writes encrypted archives to SWIRL_BACKUP_DIR;
    // 'vault' writes them as KVv2 entries under
    // <kv_mount>/data/<kv_prefix><vault_prefix>/<id>.
    storage_mode: 'fs' | 'vault';
    vault_prefix: string;
}

export interface VaultSetting {
    enabled: boolean;
    address: string;
    namespace: string;
    auth_method: 'token' | 'approle';
    token: string;
    approle_path: string;
    role_id: string;
    secret_id: string;
    kv_mount: string;
    kv_prefix: string;
    backup_key_path: string;
    backup_key_field: string;
    default_storage_mode: 'tmpfs' | 'volume' | 'init';
    tls_skip_verify: boolean;
    ca_cert: string;
    request_timeout: number;
}

export interface KeycloakGroupRole {
    group: string;
    role: string;
}

export interface KeycloakSetting {
    enabled: boolean;
    issuer_url: string;
    client_id: string;
    client_secret: string;
    redirect_uri: string;
    scopes: string;
    username_claim: string;
    email_claim: string;
    groups_claim: string;
    auto_create_user: boolean;
    group_role_map: Record<string, string>;
    enable_logout: boolean;
}

export interface DeployOptions {
    keys: {
        name: string;
        token: string;
        expiry: number;
    }[];
}

export interface LdapSetting {
    enabled: boolean;
    address: string;
    security: number;
    auth: string;
    bind_dn: string;
    bind_pwd: string;
    base_dn: string;
    user_dn: string;
    user_filter: string;
    name_attr: string;
    email_attr: string
}

export interface MetricSetting {
    prometheus: string;
}

export class SettingApi {
    load() {
        return ajax.get<Setting>('/setting/load')
    }

    save(id: string, options: Object) {
        console.log({ id, options })
        return ajax.post<Result<Object>>('/setting/save', { id, options })
    }
}

export default new SettingApi
