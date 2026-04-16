import ajax, { Result } from './ajax'

export interface Setting {
    ldap: LdapSetting;
    keycloak: KeycloakSetting;
    metric: MetricSetting;
    deploy: DeployOptions;
    vault: VaultSetting;
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
