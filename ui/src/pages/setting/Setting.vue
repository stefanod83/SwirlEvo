<template>
  <x-page-header />
  <n-space class="page-body" vertical :size="12">
    <x-panel title="LDAP" :subtitle="t('tips.ldap')" divider="bottom" :collapsed="panel !== 'ldap'">
      <template #action>
        <n-button
          secondary
          strong
          class="toggle"
          size="small"
          @click="togglePanel('ldap')"
        >{{ panel === 'ldap' ? t('buttons.collapse') : t('buttons.expand') }}</n-button>
      </template>
      <n-form
        :model="setting"
        ref="formLdap"
        label-placement="left"
        style="padding: 4px 0 0 12px"
        label-width="auto"
      >
        <n-form-item :label="t('fields.enabled')" path="ldap.enabled" label-align="right">
          <n-switch v-model:value="setting.ldap.enabled" />
        </n-form-item>
        <n-form-item :label="t('fields.address')" path="ldap.address" label-align="right">
          <n-input :placeholder="t('tips.ldap_address')" v-model:value="setting.ldap.address" />
        </n-form-item>
        <n-form-item :label="t('fields.security')" path="ldap.security">
          <n-radio-group v-model:value="setting.ldap.security">
            <n-radio :value="0">None</n-radio>
            <n-radio :value="1">TLS</n-radio>
            <n-radio :value="2">StartTLS</n-radio>
          </n-radio-group>
        </n-form-item>
        <n-form-item :label="t('fields.authentication')" path="ldap.auth">
          <n-radio-group v-model:value="setting.ldap.auth">
            <n-radio value="simple">{{ t('enums.simple') }}</n-radio>
            <n-radio value="bind">{{ t('enums.bind') }}</n-radio>
          </n-radio-group>
        </n-form-item>
        <n-form-item
          :label="t('fields.user_dn')"
          path="ldap.user_dn"
          label-align="right"
          v-show="setting.ldap.auth === 'simple'"
        >
          <n-input :placeholder="t('tips.ldap_user_dn')" v-model:value="setting.ldap.user_dn" />
        </n-form-item>
        <n-form-item
          :label="t('fields.bind_dn')"
          label-align="right"
          label-width="auto"
          :show-feedback="false"
          v-show="setting.ldap.auth === 'bind'"
        >
          <n-grid :cols="2" :x-gap="24">
            <n-form-item-gi path="ldap.bind_dn">
              <n-input-group>
                <n-input-group-label style="min-width: 60px">{{ t('fields.dn') }}</n-input-group-label>
                <n-input
                  :placeholder="t('tips.ldap_bind_dn')"
                  v-model:value="setting.ldap.bind_dn"
                />
              </n-input-group>
            </n-form-item-gi>
            <n-form-item-gi path="ldap.bind_pwd">
              <n-input-group>
                <n-input-group-label style="min-width: 60px">{{ t('fields.password') }}</n-input-group-label>
                <n-input
                  type="password"
                  :placeholder="t('tips.ldap_bind_pwd')"
                  v-model:value="setting.ldap.bind_pwd"
                />
              </n-input-group>
            </n-form-item-gi>
          </n-grid>
        </n-form-item>
        <n-form-item :label="t('fields.base_dn')" path="ldap.base_dn" label-align="right">
          <n-input :placeholder="t('tips.ldap_base_dn')" v-model:value="setting.ldap.base_dn" />
        </n-form-item>
        <n-form-item :label="t('fields.user_filter')" path="ldap.user_filter" label-align="right">
          <n-input
            :placeholder="t('tips.ldap_user_filter')"
            v-model:value="setting.ldap.user_filter"
          />
        </n-form-item>
        <n-form-item :label="t('fields.attr_map')" label-align="right" :show-feedback="false">
          <n-grid :cols="2" :x-gap="24">
            <n-form-item-gi path="ldap.name_attr">
              <n-input-group>
                <n-input-group-label style="min-width: 80px">{{ t('fields.username') }}</n-input-group-label>
                <n-input placeholder="e.g. displayName" v-model:value="setting.ldap.name_attr" />
              </n-input-group>
            </n-form-item-gi>
            <n-form-item-gi path="ldap.email_attr">
              <n-input-group>
                <n-input-group-label style="min-width: 80px">{{ t('fields.email') }}</n-input-group-label>
                <n-input placeholder="e.g. mail" v-model:value="setting.ldap.email_attr" />
              </n-input-group>
            </n-form-item-gi>
          </n-grid>
        </n-form-item>
        <n-button type="primary" @click="() => save('ldap', setting.ldap)">{{ t('buttons.save') }}</n-button>
      </n-form>
    </x-panel>
    <x-panel
      title="Keycloak"
      :subtitle="t('tips.keycloak')"
      divider="bottom"
      :collapsed="panel !== 'keycloak'"
    >
      <template #action>
        <n-button
          secondary
          strong
          class="toggle"
          size="small"
          @click="togglePanel('keycloak')"
        >{{ panel === 'keycloak' ? t('buttons.collapse') : t('buttons.expand') }}</n-button>
      </template>
      <n-alert type="info" style="margin: 4px 0 12px 0">
        {{ t('tips.keycloak_setup') }}
      </n-alert>

      <!-- Import from OpenID Configuration -->
      <n-space vertical :size="8" style="margin-bottom: 16px;">
        <n-input-group>
          <n-input
            v-model:value="kcImportInput"
            :placeholder="t('tips.kc_import_placeholder')"
            clearable
            style="flex: 1;"
          />
          <n-button type="primary" :loading="kcImporting" @click="importKeycloakConfig">
            {{ t('buttons.import') }}
          </n-button>
        </n-input-group>
        <n-alert v-if="kcImportMsg" :type="kcImportType" :show-icon="true" style="font-size: 12px;">
          {{ kcImportMsg }}
        </n-alert>
      </n-space>

      <n-form
        :model="setting"
        ref="formKeycloak"
        label-placement="left"
        style="padding: 4px 0 0 12px"
        label-width="auto"
      >
        <n-form-item :label="t('fields.enabled')" path="keycloak.enabled" label-align="right">
          <n-switch v-model:value="setting.keycloak.enabled" />
        </n-form-item>

        <n-form-item :label="t('fields.issuer_url')" path="keycloak.issuer_url" label-align="right">
          <n-input :placeholder="t('tips.kc_issuer_url_placeholder')" v-model:value="setting.keycloak.issuer_url" />
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_issuer_url_swirl') }}</div>
          <div><strong>Keycloak:</strong> {{ t('tips.kc_issuer_url_kc') }}</div>
        </div>

        <n-form-item :label="t('fields.client_id')" path="keycloak.client_id" label-align="right">
          <n-input placeholder="swirl" v-model:value="setting.keycloak.client_id" />
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_client_id_swirl') }}</div>
          <div><strong>Keycloak:</strong> {{ t('tips.kc_client_id_kc') }}</div>
        </div>

        <n-form-item :label="t('fields.client_secret')" path="keycloak.client_secret" label-align="right">
          <n-input
            type="password"
            show-password-on="click"
            :placeholder="t('tips.kc_client_secret_placeholder')"
            v-model:value="setting.keycloak.client_secret"
          />
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_client_secret_swirl') }}</div>
          <div><strong>Keycloak:</strong> {{ t('tips.kc_client_secret_kc') }}</div>
        </div>

        <n-form-item :label="t('fields.redirect_uri')" path="keycloak.redirect_uri" label-align="right">
          <n-input-group>
            <n-input readonly :value="computedRedirectURI" />
            <n-button @click="copyRedirect">{{ t('buttons.copy') }}</n-button>
          </n-input-group>
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_redirect_uri_swirl') }}</div>
          <div><strong>Keycloak:</strong> {{ t('tips.kc_redirect_uri_kc') }}</div>
        </div>

        <n-form-item :label="t('fields.scopes')" path="keycloak.scopes" label-align="right">
          <n-input placeholder="openid profile email" v-model:value="setting.keycloak.scopes" />
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_scopes_swirl') }}</div>
          <div><strong>Keycloak:</strong> {{ t('tips.kc_scopes_kc') }}</div>
        </div>

        <n-form-item :label="t('fields.username_claim')" path="keycloak.username_claim" label-align="right">
          <n-input placeholder="preferred_username" v-model:value="setting.keycloak.username_claim" />
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_username_claim_swirl') }}</div>
          <div><strong>Keycloak:</strong> {{ t('tips.kc_username_claim_kc') }}</div>
        </div>

        <n-form-item :label="t('fields.email_claim')" path="keycloak.email_claim" label-align="right">
          <n-input placeholder="email" v-model:value="setting.keycloak.email_claim" />
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_email_claim_swirl') }}</div>
          <div><strong>Keycloak:</strong> {{ t('tips.kc_email_claim_kc') }}</div>
        </div>

        <n-form-item :label="t('fields.groups_claim')" path="keycloak.groups_claim" label-align="right">
          <n-input placeholder="groups" v-model:value="setting.keycloak.groups_claim" />
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_groups_claim_swirl') }}</div>
          <div><strong>Keycloak:</strong> {{ t('tips.kc_groups_claim_kc') }}</div>
        </div>

        <n-form-item :label="t('fields.auto_create_user')" path="keycloak.auto_create_user" label-align="right">
          <n-switch v-model:value="setting.keycloak.auto_create_user" />
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_auto_create_user_swirl') }}</div>
        </div>

        <n-form-item :label="t('fields.enable_logout')" path="keycloak.enable_logout" label-align="right">
          <n-switch v-model:value="setting.keycloak.enable_logout" />
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_enable_logout_swirl') }}</div>
          <div><strong>Keycloak:</strong> {{ t('tips.kc_enable_logout_kc') }}</div>
        </div>

        <n-form-item :label="t('fields.group_role_map')" label-align="right" :show-feedback="false">
          <n-dynamic-input
            v-model:value="groupRolePairs"
            :on-create="() => ({ group: '', role: '' })"
            #="{ value }"
          >
            <n-input
              :placeholder="t('fields.group')"
              v-model:value="value.group"
              style="flex: 1"
            />
            <n-select
              :placeholder="t('fields.role')"
              v-model:value="value.role"
              :options="roleOptions"
              style="flex: 1; margin-left: 8px"
              clearable
            />
          </n-dynamic-input>
        </n-form-item>
        <div class="hint">
          <div><strong>Swirl:</strong> {{ t('tips.kc_group_role_map_swirl') }}</div>
          <div><strong>Keycloak:</strong> {{ t('tips.kc_group_role_map_kc') }}</div>
        </div>

        <n-space>
          <n-button type="primary" @click="saveKeycloak">{{ t('buttons.save') }}</n-button>
          <n-button :loading="kcTesting" @click="testKeycloak">{{ t('buttons.test_connection') }}</n-button>
        </n-space>
        <n-alert
          v-if="kcTestResult"
          :type="kcTestAllOk ? 'success' : 'error'"
          style="margin-top: 12px;"
          :title="kcTestAllOk ? 'Keycloak OK' : 'Keycloak diagnostic'"
        >
          <div v-for="(check, key) of kcTestResult" :key="key" style="margin-bottom: 8px;">
            <strong>{{ key }}:</strong>
            <n-tag size="small" :type="check.ok ? 'success' : 'error'" style="margin-left: 4px;">
              {{ check.ok ? 'OK' : 'FAIL' }}
            </n-tag>
            <div v-if="check.error" style="margin-top: 2px; font-size: 12px; opacity: 0.8;">
              <code>{{ check.error }}</code>
            </div>
            <div v-if="check.authEndpoint" style="font-size: 12px; opacity: 0.7;">
              Auth: <code>{{ check.authEndpoint }}</code>
            </div>
            <div v-if="check.configured" style="font-size: 12px; opacity: 0.7;">
              Redirect URI: <code>{{ check.configured }}</code>
            </div>
            <div v-if="check.hint" style="font-size: 12px; opacity: 0.7;">
              {{ check.hint }}
            </div>
          </div>
        </n-alert>
      </n-form>
    </x-panel>
    <x-panel
      title="HashiCorp Vault"
      :subtitle="t('tips.vault')"
      divider="bottom"
      :collapsed="panel !== 'vault'"
    >
      <template #action>
        <n-button
          secondary
          strong
          class="toggle"
          size="small"
          @click="togglePanel('vault')"
        >{{ panel === 'vault' ? t('buttons.collapse') : t('buttons.expand') }}</n-button>
      </template>
      <n-alert type="info" style="margin: 4px 0 12px 0">
        {{ t('tips.vault_setup') }}
      </n-alert>
      <n-form
        :model="setting"
        ref="formVault"
        label-placement="left"
        style="padding: 4px 0 0 12px"
        label-width="auto"
      >
        <n-form-item :label="t('fields.enabled')" path="vault.enabled" label-align="right">
          <n-switch v-model:value="setting.vault.enabled" />
        </n-form-item>
        <n-form-item :label="t('fields.address')" path="vault.address" label-align="right">
          <n-input placeholder="https://vault.example.com:8200" v-model:value="setting.vault.address" />
        </n-form-item>
        <n-form-item :label="t('fields.namespace')" path="vault.namespace" label-align="right">
          <n-input :placeholder="t('tips.vault_namespace')" v-model:value="setting.vault.namespace" />
        </n-form-item>
        <n-form-item :label="t('fields.auth_method')" path="vault.auth_method">
          <n-radio-group v-model:value="setting.vault.auth_method">
            <n-radio value="token">Token</n-radio>
            <n-radio value="approle">AppRole</n-radio>
          </n-radio-group>
        </n-form-item>
        <n-form-item
          :label="t('fields.token')"
          path="vault.token"
          label-align="right"
          v-show="setting.vault.auth_method === 'token'"
        >
          <n-input
            type="password"
            show-password-on="click"
            :placeholder="t('tips.vault_token')"
            v-model:value="setting.vault.token"
          />
        </n-form-item>
        <n-form-item
          :label="t('fields.approle_path')"
          path="vault.approle_path"
          label-align="right"
          v-show="setting.vault.auth_method === 'approle'"
        >
          <n-input placeholder="approle" v-model:value="setting.vault.approle_path" />
        </n-form-item>
        <n-form-item
          :label="t('fields.role_id')"
          path="vault.role_id"
          label-align="right"
          v-show="setting.vault.auth_method === 'approle'"
        >
          <n-input v-model:value="setting.vault.role_id" />
        </n-form-item>
        <n-form-item
          :label="t('fields.secret_id')"
          path="vault.secret_id"
          label-align="right"
          v-show="setting.vault.auth_method === 'approle'"
        >
          <n-input
            type="password"
            show-password-on="click"
            v-model:value="setting.vault.secret_id"
          />
        </n-form-item>
        <n-form-item :label="t('fields.kv_mount')" path="vault.kv_mount" label-align="right">
          <n-input placeholder="secret" v-model:value="setting.vault.kv_mount" />
        </n-form-item>
        <n-form-item :label="t('fields.kv_prefix')" path="vault.kv_prefix" label-align="right">
          <n-input placeholder="swirl/" v-model:value="setting.vault.kv_prefix" />
        </n-form-item>
        <n-form-item :label="t('fields.backup_key_path')" path="vault.backup_key_path" label-align="right">
          <n-input placeholder="backup-key" v-model:value="setting.vault.backup_key_path" />
        </n-form-item>
        <n-form-item :label="t('fields.backup_key_field')" path="vault.backup_key_field" label-align="right">
          <n-input placeholder="value" v-model:value="setting.vault.backup_key_field" />
        </n-form-item>
        <n-form-item :label="t('fields.default_storage_mode')" path="vault.default_storage_mode">
          <n-radio-group v-model:value="setting.vault.default_storage_mode">
            <n-radio value="tmpfs">{{ t('enums.storage_tmpfs') }}</n-radio>
            <n-radio value="volume">{{ t('enums.storage_volume') }}</n-radio>
            <n-radio value="init">{{ t('enums.storage_init') }}</n-radio>
          </n-radio-group>
        </n-form-item>
        <n-form-item :label="t('fields.tls_skip_verify')" path="vault.tls_skip_verify" label-align="right">
          <n-switch v-model:value="setting.vault.tls_skip_verify" />
        </n-form-item>
        <n-form-item :label="t('fields.ca_cert')" path="vault.ca_cert" label-align="right">
          <n-input
            type="textarea"
            :autosize="{ minRows: 3, maxRows: 8 }"
            placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
            v-model:value="setting.vault.ca_cert"
          />
        </n-form-item>
        <n-form-item :label="t('fields.request_timeout')" path="vault.request_timeout" label-align="right">
          <n-input-number
            :min="1"
            :max="120"
            v-model:value="setting.vault.request_timeout"
          />
        </n-form-item>
        <n-space>
          <n-button type="primary" @click="() => save('vault', setting.vault)">{{ t('buttons.save') }}</n-button>
          <n-button :loading="vaultTesting" @click="testVault">{{ t('buttons.test_connection') }}</n-button>
        </n-space>
        <n-alert
          v-if="vaultTestMsg"
          :type="vaultTestType"
          style="margin-top: 12px"
        >{{ vaultTestMsg }}</n-alert>
      </n-form>
    </x-panel>
    <x-panel
      :title="t('backup.storage_panel_title')"
      :subtitle="t('backup.storage_panel_subtitle')"
      divider="bottom"
      :collapsed="panel !== 'backup_storage'"
    >
      <template #action>
        <n-button
          secondary
          strong
          class="toggle"
          size="small"
          @click="togglePanel('backup_storage')"
        >{{ panel === 'backup_storage' ? t('buttons.collapse') : t('buttons.expand') }}</n-button>
      </template>
      <n-form
        :model="setting"
        label-placement="left"
        style="padding: 4px 0 0 12px"
      >
        <n-form-item :label="t('backup.storage_mode')" path="backup.storage_mode" label-align="right">
          <n-radio-group v-model:value="setting.backup.storage_mode">
            <n-radio value="fs">{{ t('backup.storage_fs') }}</n-radio>
            <n-radio value="vault">{{ t('backup.storage_vault') }}</n-radio>
            <n-radio value="db">{{ t('backup.storage_db') }}</n-radio>
          </n-radio-group>
        </n-form-item>
        <n-form-item
          v-if="setting.backup.storage_mode === 'vault'"
          :label="t('backup.vault_prefix')"
          path="backup.vault_prefix"
          label-align="right"
        >
          <n-input
            placeholder="backups"
            v-model:value="setting.backup.vault_prefix"
          />
        </n-form-item>
        <n-alert
          v-if="setting.backup.storage_mode === 'vault'"
          type="info"
          :show-icon="false"
          style="margin-bottom: 12px;"
        >
          {{ t('backup.storage_vault_hint') }}
        </n-alert>
        <n-button type="primary" @click="() => save('backup', setting.backup)">{{ t('buttons.save') }}</n-button>
      </n-form>
    </x-panel>
    <!-- Federation panel: mint / rotate / revoke peer tokens that
         authenticate remote Swirl portals federating against this
         instance. Only meaningful on the target side (MODE=swarm) —
         the standalone portal consumes these tokens, it does not
         mint them, so the panel is hidden there. -->
    <x-panel
      v-if="canAdminFederation && store.state.mode === 'swarm'"
      :title="t('federation.title')"
      :subtitle="t('federation.subtitle')"
      divider="bottom"
      :collapsed="panel !== 'federation'"
    >
      <template #action>
        <n-button
          secondary
          strong
          class="toggle"
          size="small"
          @click="togglePanel('federation')"
        >{{ panel === 'federation' ? t('buttons.collapse') : t('buttons.expand') }}</n-button>
      </template>
      <n-space vertical :size="12" style="padding: 4px 0 0 12px">
        <n-space :size="8">
          <n-button type="primary" size="small" @click="fedCreateOpen = true">
            {{ t('federation.create_peer') }}
          </n-button>
          <n-button secondary size="small" @click="loadFederationPeers" :loading="fedLoading">
            {{ t('buttons.refresh') }}
          </n-button>
        </n-space>
        <n-data-table
          v-if="fedPeers.length > 0"
          :columns="fedColumns"
          :data="fedPeers"
          :row-key="(r: any) => r.id"
          size="small"
          :bordered="true"
        />
        <div v-else class="sd-muted" style="padding: 8px 4px; font-size: 13px">
          {{ t('federation.empty_hint') }}
        </div>
      </n-space>
    </x-panel>

    <!-- Create peer modal: captures the one-time token. After save,
         the body switches to a copy-once display — closing the modal
         without copying means losing the token irrecoverably. -->
    <n-modal
      v-model:show="fedCreateOpen"
      preset="card"
      :title="t('federation.create_peer')"
      style="max-width: 560px"
      :mask-closable="!fedCreated"
    >
      <n-space vertical :size="12">
        <div v-if="!fedCreated">
          <n-form :model="fedCreateModel" label-placement="left" label-width="110">
            <n-form-item :label="t('fields.name')" required>
              <n-input v-model:value="fedCreateModel.name" placeholder="portal-1" />
            </n-form-item>
            <n-form-item :label="t('federation.ttl_days')">
              <n-input-number v-model:value="fedCreateModel.ttlDays" :min="0" :max="3650" style="width: 100%" />
            </n-form-item>
          </n-form>
          <div class="sd-muted" style="font-size: 12px">
            {{ t('federation.ttl_hint') }}
          </div>
          <n-space style="margin-top: 12px" justify="end">
            <n-button @click="fedCreateOpen = false">{{ t('buttons.cancel') }}</n-button>
            <n-button type="primary" :loading="fedCreating" @click="submitCreatePeer">
              {{ t('federation.generate') }}
            </n-button>
          </n-space>
        </div>
        <div v-else>
          <n-alert type="warning" :show-icon="true">
            {{ t('federation.token_once_warning') }}
          </n-alert>
          <div style="margin-top: 10px">
            <div class="sd-muted" style="font-size: 12px">
              {{ t('federation.peer') }}: <code>{{ fedCreated.name }}</code>
              · {{ t('fields.token_expires_at') }}: {{ fedCreated.expiresAt ? new Date(fedCreated.expiresAt * 1000).toLocaleString() : '—' }}
            </div>
            <n-input
              type="textarea"
              :value="fedCreated.token"
              readonly
              :autosize="{ minRows: 3, maxRows: 3 }"
              style="margin-top: 8px; font-family: monospace"
            />
          </div>
          <n-space style="margin-top: 12px" justify="end">
            <n-button @click="copyToken(fedCreated.token)">{{ t('buttons.copy') }}</n-button>
            <n-button type="primary" @click="closeCreatePeerModal">{{ t('buttons.close') }}</n-button>
          </n-space>
        </div>
      </n-space>
    </n-modal>

    <x-panel
      :title="t('self_deploy.title')"
      :subtitle="t('self_deploy.subtitle')"
      divider="bottom"
      :collapsed="panel !== 'self_deploy'"
    >
      <template #action>
        <n-button
          secondary
          strong
          class="toggle"
          size="small"
          @click="togglePanel('self_deploy')"
        >{{ panel === 'self_deploy' ? t('buttons.collapse') : t('buttons.expand') }}</n-button>
      </template>
      <n-space vertical :size="16" style="padding: 4px 0 0 12px">
        <!-- Block 1: Stack flag — enable + select the Swirl compose stack.
             Everything else (YAML, env, bindings) is edited via the normal
             compose_stack pages. -->
        <div>
          <n-form
            :model="selfDeploy"
            label-placement="left"
            label-width="auto"
            :show-feedback="false"
            size="small"
          >
            <n-form-item :label="t('self_deploy.enabled')" label-align="right">
              <n-switch v-model:value="selfDeploy.enabled" />
            </n-form-item>
            <n-form-item :label="t('self_deploy.source_stack')" label-align="right">
              <n-select
                v-model:value="selfDeploy.sourceStackId"
                :options="sourceStackOptions"
                :loading="sdSourceStackLoading"
                :placeholder="t('self_deploy.source_stack')"
                style="min-width: 320px;"
                clearable
              />
            </n-form-item>
          </n-form>
          <div class="sd-hint" style="margin-left: 6px;">{{ t('self_deploy.source_stack_hint') }}</div>
        </div>

        <!-- Block 2: Sidekick options — how the deploy-agent behaves. -->
        <div>
          <n-form
            :model="selfDeploy"
            label-placement="left"
            label-width="auto"
            :show-feedback="false"
            size="small"
          >
            <n-form-item :label="t('self_deploy.auto_rollback')" label-align="right">
              <n-switch v-model:value="selfDeploy.autoRollback" />
            </n-form-item>
            <n-form-item :label="t('self_deploy.deploy_timeout')" label-align="right">
              <n-input-number
                :min="60"
                :max="1800"
                v-model:value="selfDeploy.deployTimeout"
                style="width: 100%"
              />
            </n-form-item>
          </n-form>
        </div>

        <!-- Save -->
        <n-space>
          <n-button
            v-if="canEditSelfDeploy"
            type="primary"
            :loading="sdSaving"
            @click="saveSelfDeploy"
          >{{ t('buttons.save') }}</n-button>
        </n-space>

        <n-alert
          v-if="sdSaveError"
          type="error"
          :show-icon="true"
        >{{ sdSaveError }}</n-alert>

        <!-- Status panel -->
        <div v-if="sdStatus" class="sd-status-panel">
          <div class="sd-block-title">{{ t('self_deploy.status.title') }}</div>
          <n-space :size="12" align="center" style="margin-bottom: 8px">
            <n-tag :type="phaseTagType(sdStatus.phase)" round>
              {{ phaseLabel(sdStatus.phase) }}
            </n-tag>
            <span v-if="sdStatus.jobId" class="sd-muted">
              {{ t('self_deploy.status.job_id') }}: <code>{{ sdStatus.jobId }}</code>
            </span>
          </n-space>
          <n-alert
            v-if="sdStatus.error"
            type="error"
            :show-icon="true"
            style="margin-bottom: 8px"
          >{{ sdStatus.error }}</n-alert>
          <!-- Reset (Clear stuck lock) — visible when the on-disk state
               is in a stale in-progress phase but the sidekick is dead. -->
          <n-space
            v-if="sdStatus.canReset && canEditSelfDeploy"
            :size="8"
            align="center"
            style="margin-bottom: 8px"
          >
            <n-alert type="warning" :show-icon="true" style="flex: 1">
              {{ t('self_deploy.reset.hint') }}
            </n-alert>
            <n-popconfirm
              :positive-text="t('self_deploy.reset.button')"
              :negative-text="t('buttons.cancel')"
              @positive-click="resetSelfDeploy"
            >
              <template #trigger>
                <n-button type="warning" :loading="sdResetting">
                  {{ t('self_deploy.reset.button') }}
                </n-button>
              </template>
              {{ t('self_deploy.reset.confirm') }}
            </n-popconfirm>
          </n-space>
          <!-- Sidekick info -->
          <div v-if="sdStatus.sidekickContainer" class="sd-muted" style="margin-bottom: 6px">
            {{ t('self_deploy.status.sidekick_container') }}:
            <code>{{ sdStatus.sidekickContainer }}</code>
            <span v-if="sdStatus.sidekickAlive" style="margin-left: 8px; color: var(--n-text-color-3, #999)">
              · {{ t('self_deploy.status.sidekick_alive') }}
            </span>
            <span v-else style="margin-left: 8px; color: var(--n-text-color-3, #999)">
              · {{ t('self_deploy.status.sidekick_dead') }}
            </span>
          </div>
          <div class="sd-block-title">{{ t('self_deploy.status.log_tail') }}</div>
          <pre class="sd-log">{{ logTailText || t('self_deploy.status.no_logs') }}</pre>
          <!-- Docker logs of the sidekick container: captured by the
               biz layer on every status poll. Useful when the sidekick
               crashed before writing any state.json update. -->
          <div
            v-if="sdStatus.sidekickLogs"
            class="sd-block-title"
            style="margin-top: 10px"
          >{{ t('self_deploy.status.sidekick_logs') }}</div>
          <pre
            v-if="sdStatus.sidekickLogs"
            class="sd-log"
          >{{ sdStatus.sidekickLogs }}</pre>
        </div>
      </n-space>
    </x-panel>

    <!-- Deploy-in-progress modal: no iframe. Polls /api/system/mode
         for readiness + /api/self-deploy/status for live phase & logs.
         Closes + reloads the page when the new Swirl answers 200. -->
    <n-modal
      v-model:show="progressOpen"
      :mask-closable="false"
      :closable="false"
      preset="card"
      :bordered="false"
      style="max-width: 640px;"
    >
      <template #header>
        <n-space align="center" :size="8">
          <n-spin size="small" />
          <span>{{ t('self_deploy.progress.title') }}</span>
        </n-space>
      </template>
      <template #header-extra>
        <n-tag v-if="progressTimedOut" type="warning" size="small" round>
          {{ t('self_deploy.progress.timeout') }}
        </n-tag>
      </template>
      <n-space vertical :size="12" style="padding: 8px 4px;">
        <div style="font-size: 14px; line-height: 1.5">
          {{ progressDescription }}
        </div>
        <n-space :size="8" align="center">
          <n-tag
            v-if="progressPhase"
            :type="phaseTagType(progressPhase)"
            round
            size="small"
          >
            {{ progressPhaseLabel }}
          </n-tag>
          <span class="sd-muted" style="font-size: 13px">{{ progressStatus }}</span>
          <span v-if="progressElapsed" class="sd-muted" style="font-size: 12px; opacity: 0.7">({{ progressElapsed }})</span>
        </n-space>
        <div v-if="currentJobId" class="sd-muted" style="font-size: 12px">
          {{ t('self_deploy.status.job_id') }}: <code>{{ currentJobId }}</code>
        </div>
        <n-alert
          v-if="progressError"
          type="error"
          :show-icon="true"
          style="font-size: 12px"
        >{{ progressError }}</n-alert>
        <pre
          v-if="progressLogTail.length"
          class="sd-log"
          style="max-height: 260px; overflow: auto; font-size: 12px; margin: 0"
        >{{ progressLogTail.join('\n') }}</pre>
        <!-- Manual "Reload now" escape hatch shown when the new primary
             is up (phase=success) but the SPA bundle still isn't being
             served within the grace window. Operator decides whether
             to reload on a possibly-half-ready server. -->
        <n-space v-if="readyStuck" justify="end">
          <n-button type="primary" size="small" @click="reloadNow">
            {{ t('self_deploy.progress.reload_now') }}
          </n-button>
        </n-space>
      </n-space>
    </n-modal>

    <!-- Registry Cache: global configuration of the operator-deployed
         pull-through mirror. Swirl stores connection info + upstream
         prefix mapping here, distributes the CA via bootstrap script
         (Phase 2), and rewrites compose image: references at deploy
         time (Phase 3). -->
    <!-- No outer permission gate: the panel renders for everyone that
         can reach the Settings page, matching the LDAP / Keycloak /
         Vault / SelfDeploy panels. Save + Gen CA buttons below carry
         the `registry_cache.edit` guard so read-only viewers simply
         see the current state without the write controls. -->
    <x-panel
      :title="t('registry_cache.title')"
      :subtitle="t('registry_cache.subtitle')"
      divider="bottom"
      :collapsed="panel !== 'registry_cache'"
    >
      <template #action>
        <n-button
          secondary
          strong
          class="toggle"
          size="small"
          @click="togglePanel('registry_cache')"
        >{{ panel === 'registry_cache' ? t('buttons.collapse') : t('buttons.expand') }}</n-button>
      </template>
      <n-alert type="info" style="margin: 4px 0 12px 0">
        {{ t('registry_cache.tip') }}
      </n-alert>
      <!-- Render guard: the reactive state defines registry_cache in
           the initial ref + fetchData hydrates it with defensive
           defaults, so this branch should never show. It exists as a
           last-resort diagnostic if a future refactor accidentally
           breaks the reactive chain. -->
      <n-alert v-if="!setting.registry_cache" type="error" style="margin-bottom: 12px">
        Internal error: registry_cache state is missing. Check the browser console.
      </n-alert>
      <n-form
        v-if="setting.registry_cache"
        :model="setting"
        ref="formRegistryCache"
        label-placement="left"
        style="padding: 4px 0 0 12px"
        label-width="auto"
      >
        <n-form-item :label="t('fields.enabled')" path="registry_cache.enabled" label-align="right">
          <n-switch v-model:value="setting.registry_cache.enabled" />
        </n-form-item>
        <!-- Link to Registry catalog: single source of truth for
             hostname / port / credentials / CA. When selected, the
             inline mirror fields become read-only (overlayed on Save
             from the Registry). Clearing reverts to inline mode. -->
        <n-form-item :label="t('registry_cache.linked_registry')" path="registry_cache.registry_id" label-align="right">
          <n-select
            v-model:value="setting.registry_cache.registry_id"
            :options="registryOptions"
            :placeholder="t('registry_cache.linked_registry_placeholder')"
            clearable
            style="min-width: 320px"
          />
        </n-form-item>
        <div v-if="rcIsLinked" class="hint">
          <strong>{{ t('registry_cache.linked_registry') }}:</strong>
          {{ t('registry_cache.linked_registry_hint') }}
        </div>
        <n-form-item :label="t('registry_cache.hostname')" path="registry_cache.hostname" label-align="right">
          <n-input
            :placeholder="t('registry_cache.hostname_placeholder')"
            :disabled="rcIsLinked"
            v-model:value="setting.registry_cache.hostname"
          />
        </n-form-item>
        <n-form-item :label="t('registry_cache.port')" path="registry_cache.port" label-align="right">
          <n-input-number
            :min="1"
            :max="65535"
            :disabled="rcIsLinked"
            v-model:value="setting.registry_cache.port"
            style="width: 160px"
          />
        </n-form-item>
        <n-form-item :label="t('registry_cache.rewrite_mode')" path="registry_cache.rewrite_mode">
          <n-radio-group v-model:value="setting.registry_cache.rewrite_mode">
            <n-radio value="off">{{ t('registry_cache.rewrite_off') }}</n-radio>
            <n-radio value="per-host">{{ t('registry_cache.rewrite_per_host') }}</n-radio>
            <n-radio value="always">{{ t('registry_cache.rewrite_always') }}</n-radio>
          </n-radio-group>
        </n-form-item>
        <n-form-item
          :label="t('registry_cache.preserve_digests')"
          path="registry_cache.preserve_digests"
          label-align="right"
        >
          <n-switch v-model:value="setting.registry_cache.preserve_digests" />
        </n-form-item>
        <div class="hint">{{ t('registry_cache.preserve_digests_hint') }}</div>

        <n-form-item :label="t('registry_cache.username')" path="registry_cache.username" label-align="right">
          <n-input
            :placeholder="t('registry_cache.username_placeholder')"
            :disabled="rcIsLinked"
            v-model:value="setting.registry_cache.username"
          />
        </n-form-item>
        <n-form-item :label="t('fields.password')" path="registry_cache.password" label-align="right">
          <n-input
            type="password"
            show-password-on="click"
            :placeholder="t('registry_cache.password_placeholder')"
            :disabled="rcIsLinked"
            v-model:value="setting.registry_cache.password"
          />
        </n-form-item>

        <n-form-item :label="t('registry_cache.ca_cert_pem')" path="registry_cache.ca_cert_pem" label-align="right">
          <n-input
            type="textarea"
            :autosize="{ minRows: 3, maxRows: 10 }"
            placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
            :disabled="rcIsLinked"
            v-model:value="setting.registry_cache.ca_cert_pem"
          />
        </n-form-item>
        <div v-if="setting.registry_cache.ca_fingerprint" class="hint">
          <strong>{{ t('registry_cache.ca_fingerprint') }}:</strong>
          <code style="font-size: 11px; word-break: break-all">{{ setting.registry_cache.ca_fingerprint }}</code>
        </div>

        <!-- Live ping probe: POST /api/registry-cache/ping. Operator
             can verify the mirror is reachable without leaving the
             Settings tab. Badge updates on-demand (click) so we do
             not hammer the mirror every page load. -->
        <n-space
          v-if="setting.registry_cache.enabled"
          :size="8"
          align="center"
          style="margin-top: 8px; margin-bottom: 8px"
        >
          <n-button size="small" :loading="rcPinging" @click="pingMirror">
            {{ t('registry_cache.ping_btn') }}
          </n-button>
          <n-tag
            v-if="rcPingResult"
            :type="rcPingResult.ok ? 'success' : 'error'"
            size="small"
            round
          >
            {{ rcPingResult.ok
              ? t('registry_cache.ping_ok', { status: rcPingResult.status, ms: rcPingResult.latencyMs || 0 })
              : t('registry_cache.ping_fail', { error: rcPingResult.error || String(rcPingResult.status) }) }}
          </n-tag>
        </n-space>

        <n-form-item
          :label="t('registry_cache.use_upstream_prefix')"
          path="registry_cache.use_upstream_prefix"
          label-align="right"
        >
          <n-switch v-model:value="setting.registry_cache.use_upstream_prefix" />
        </n-form-item>
        <div class="hint">{{ t('registry_cache.use_upstream_prefix_hint') }}</div>

        <n-space style="margin-top: 12px">
          <n-button
            v-if="canEditRegistryCache"
            type="primary"
            @click="saveRegistryCache"
          >{{ t('buttons.save') }}</n-button>
          <n-button
            v-if="canEditRegistryCache && !rcIsLinked"
            :loading="rcGenerating"
            @click="openGenCA"
          >{{ t('registry_cache.gen_ca') }}</n-button>
        </n-space>
      </n-form>
    </x-panel>

    <!-- CA generation modal. The private key is returned ONCE by the
         backend and never persisted; closing without downloading means
         generating a fresh pair next time. -->
    <n-modal
      v-model:show="rcGenOpen"
      preset="card"
      :title="t('registry_cache.gen_ca_title')"
      style="max-width: 720px"
      :mask-closable="false"
    >
      <n-space vertical :size="12">
        <n-alert type="warning" :show-icon="true">
          {{ t('registry_cache.gen_ca_warning') }}
        </n-alert>
        <div>
          <div class="sd-block-title">{{ t('registry_cache.ca_cert_pem') }}</div>
          <n-input
            type="textarea"
            :value="rcGenResult?.certPEM || ''"
            readonly
            :autosize="{ minRows: 4, maxRows: 8 }"
            style="font-family: monospace; font-size: 11px"
          />
          <n-space style="margin-top: 6px">
            <n-button size="tiny" @click="copyText(rcGenResult?.certPEM || '')">{{ t('buttons.copy') }}</n-button>
            <n-button size="tiny" @click="downloadText(rcGenResult?.certPEM || '', 'swirl-registry-ca.crt')">
              {{ t('registry_cache.download_cert') }}
            </n-button>
          </n-space>
        </div>
        <div>
          <div class="sd-block-title">{{ t('registry_cache.ca_key_pem') }}</div>
          <n-input
            type="textarea"
            :value="rcGenResult?.keyPEM || ''"
            readonly
            :autosize="{ minRows: 4, maxRows: 8 }"
            style="font-family: monospace; font-size: 11px"
          />
          <n-space style="margin-top: 6px">
            <n-button size="tiny" @click="copyText(rcGenResult?.keyPEM || '')">{{ t('buttons.copy') }}</n-button>
            <n-button size="tiny" @click="downloadText(rcGenResult?.keyPEM || '', 'swirl-registry-ca.key')">
              {{ t('registry_cache.download_key') }}
            </n-button>
          </n-space>
        </div>
        <n-space justify="end" style="margin-top: 12px">
          <n-button @click="rcGenOpen = false">{{ t('buttons.close') }}</n-button>
          <n-button type="primary" @click="applyGeneratedCert">
            {{ t('registry_cache.use_cert') }}
          </n-button>
        </n-space>
      </n-space>
    </n-modal>

    <x-panel
      :title="t('fields.monitor')"
      :subtitle="t('tips.monitor')"
      :collapsed="panel !== 'metric'"
    >
      <template #action>
        <n-button
          secondary
          strong
          class="toggle"
          size="small"
          @click="togglePanel('metric')"
        >{{ panel === 'metric' ? t('buttons.collapse') : t('buttons.expand') }}</n-button>
      </template>
      <n-form
        :model="setting"
        ref="formMetrics"
        label-placement="left"
        style="padding: 4px 0 0 12px"
      >
        <n-form-item label="Prometheus" path="metric.prometheus" label-align="right">
          <n-input :placeholder="t('tips.prometheus')" v-model:value="setting.metric.prometheus" />
        </n-form-item>
        <n-button
          type="primary"
          @click="() => save('metric', setting.metric)"
        >{{ t('buttons.save') }}</n-button>
      </n-form>
    </x-panel>
    <n-alert type="info">{{ t('texts.setting_notice') }}</n-alert>
  </n-space>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import {
  NGrid,
  NButton,
  NSpace,
  NInput,
  NInputGroup,
  NInputGroupLabel,
  NInputNumber,
  NForm,
  NFormItem,
  NFormItemGi,
  NRadioGroup,
  NRadio,
  NSwitch,
  NAlert,
  NDynamicInput,
  NSelect,
  NTag,
  NModal,
  NPopconfirm,
  NSpin,
  NDataTable,
} from "naive-ui";
import XPageHeader from "@/components/PageHeader.vue";
import XPanel from "@/components/Panel.vue";
import settingApi from "@/api/setting";
import { store } from "@/store";
import type { Setting } from "@/api/setting";
import vaultApi from "@/api/vault";
import roleApi from "@/api/role";
import selfDeployApi, {
  defaultConfig as sdDefaultConfig,
  type SelfDeployConfig,
  type SelfDeployStatus,
} from "@/api/self-deploy";
import composeStackApi, { type ComposeStackSummary } from "@/api/compose_stack";
import { useAutoDeployProgress } from "@/composables/useAutoDeployProgress";
import registryCacheApi, { type GenCAResult, type PingResult } from "@/api/registry-cache";
import registryApi from "@/api/registry";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

// Defaults for the Registry Cache settings subtree. Kept as a pure
// function so we can re-apply on initial state + after every load
// without accidentally sharing a mutable reference with the reactive
// state.
function rcDefaults() {
  return {
    enabled: false,
    registry_id: '',
    hostname: '',
    port: 5000,
    ca_cert_pem: '',
    ca_fingerprint: '',
    username: '',
    password: '',
    use_upstream_prefix: true,
    rewrite_mode: 'per-host' as 'off' | 'per-host' | 'always',
    preserve_digests: true,
  }
}

const setting = ref({
  ldap: {
    security: 0,
    auth: 'simple',
  },
  keycloak: {
    enabled: false,
    issuer_url: '',
    client_id: '',
    client_secret: '',
    redirect_uri: '',
    scopes: 'openid profile email',
    username_claim: 'preferred_username',
    email_claim: 'email',
    groups_claim: 'groups',
    auto_create_user: false,
    group_role_map: {},
    enable_logout: false,
  },
  metric: {},
  deploy: {},
  vault: {
    enabled: false,
    address: '',
    namespace: '',
    auth_method: 'token',
    token: '',
    approle_path: 'approle',
    role_id: '',
    secret_id: '',
    kv_mount: 'secret',
    kv_prefix: 'swirl/',
    backup_key_path: 'backup-key',
    backup_key_field: 'value',
    default_storage_mode: 'tmpfs',
    tls_skip_verify: false,
    ca_cert: '',
    request_timeout: 10,
  },
  backup: {
    storage_mode: 'fs',
    vault_prefix: 'backups',
  },
  registry_cache: rcDefaults(),
// The literal above is a partial defaults template — fields get
// hydrated from the backend in fetchData(). The cast is kept
// intentional-partial (via unknown) so TS does not block on missing
// nested fields that already live on Setting's required subtrees.
} as unknown as Setting);
const panel = ref('')

// --- Federation peers management (target-side: Swirl in swarm mode
//     typically, but the API accepts it in any mode; gating is via
//     the `federation.admin` permission set on the user's role.) ---
import { h as fedH } from 'vue'
import { NPopconfirm as FedPopconfirm, NButton as FedButton, NTag as FedTag } from 'naive-ui'
import federationApi from '@/api/federation'
import type { FederationPeer, FederationPeerResult } from '@/api/federation'

const canAdminFederation = computed(() => store.getters.allow('federation.admin'))
const canViewRegistryCache = computed(() => store.getters.allow('registry_cache.view'))
const canEditRegistryCache = computed(() => store.getters.allow('registry_cache.edit'))

// Gen-CA modal state. The private key comes back from the backend once
// and is never persisted by Swirl — the operator downloads it to sign
// the mirror's server cert offline. Only the public cert ends up in
// Setting.registry_cache.ca_cert_pem.
const rcGenOpen = ref(false)
const rcGenerating = ref(false)
const rcGenResult = ref<GenCAResult | null>(null)

const rcPinging = ref(false)
const rcPingResult = ref<PingResult | null>(null)

// Registry catalog options bound to the "Link to Registry" selector.
// Populated at mount from /api/registry/search. When the operator
// selects one, its id is stored in setting.registry_cache.registry_id
// and the overlay on Save pulls hostname/port/credentials/CA from
// the Registry entity.
const registryOptions = ref<{ label: string; value: string }[]>([])
const rcIsLinked = computed(() => !!setting.value.registry_cache?.registry_id)

async function loadRegistryOptions() {
  try {
    const r = await registryApi.search()
    registryOptions.value = (r.data || []).map(reg => ({
      label: `${reg.name} (${reg.url})`,
      value: reg.id,
    }))
  } catch { /* silent — the dropdown just stays empty */ }
}

async function pingMirror() {
  rcPinging.value = true
  try {
    const r = await registryCacheApi.ping()
    rcPingResult.value = (r.data ?? null) as PingResult | null
  } catch (e: any) {
    rcPingResult.value = { ok: false, error: e?.response?.data?.info || e?.message || String(e) }
  } finally {
    rcPinging.value = false
  }
}


async function openGenCA() {
  rcGenerating.value = true
  try {
    const r = await registryCacheApi.genCA(setting.value.registry_cache?.hostname || '')
    rcGenResult.value = r.data ?? null
    rcGenOpen.value = true
  } catch (e: any) {
    window.message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    rcGenerating.value = false
  }
}

async function saveRegistryCache() {
  await save('registry_cache', setting.value.registry_cache)
}

function applyGeneratedCert() {
  if (rcGenResult.value?.certPEM) {
    setting.value.registry_cache.ca_cert_pem = rcGenResult.value.certPEM
  }
  rcGenOpen.value = false
  window.message.info(t('registry_cache.cert_applied'))
}

function copyText(s: string) {
  if (!s) return
  try {
    navigator.clipboard.writeText(s)
    window.message.success(t('texts.action_success'))
  } catch {
    window.message.error('copy failed')
  }
}

function downloadText(s: string, filename: string) {
  if (!s) return
  const blob = new Blob([s], { type: 'application/x-pem-file' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
const fedPeers = ref<FederationPeer[]>([])
const fedLoading = ref(false)
const fedCreateOpen = ref(false)
const fedCreating = ref(false)
const fedCreated = ref<FederationPeerResult | null>(null)
const fedCreateModel = reactive({ name: '', ttlDays: 90 })

const fedColumns = computed(() => [
  { title: t('fields.name'), key: 'name' },
  { title: t('federation.login_name'), key: 'loginName', render: (r: FederationPeer) => fedH('code', { style: 'font-size: 11px' }, r.loginName) },
  {
    title: t('fields.token_expires_at'), key: 'expiresAt',
    render: (r: FederationPeer) => {
      if (!r.expiresAt) return '—'
      const d = new Date(r.expiresAt * 1000)
      return fedH('span', { class: r.expired ? 'sd-muted' : '' }, d.toLocaleString())
    },
  },
  {
    title: t('fields.token_status'), key: 'status',
    render: (r: FederationPeer) => fedH(FedTag, {
      type: r.expired ? 'error' : 'success',
      size: 'small',
      round: true,
    }, { default: () => r.expired ? t('fields.token_expired') : t('fields.token_valid') }),
  },
  {
    title: t('fields.actions'), key: 'actions', width: 220,
    render: (r: FederationPeer) => fedH('div', { style: 'display: flex; gap: 6px' }, [
      fedH(FedButton, {
        size: 'tiny',
        quaternary: true,
        type: 'warning',
        onClick: () => rotateFederationPeer(r),
      }, { default: () => t('federation.rotate') }),
      fedH(FedPopconfirm, {
        showIcon: false,
        onPositiveClick: () => revokeFederationPeer(r),
      }, {
        default: () => t('federation.revoke_confirm'),
        trigger: () => fedH(FedButton, { size: 'tiny', quaternary: true, type: 'error' },
          { default: () => t('federation.revoke') }),
      }),
    ]),
  },
])

async function loadFederationPeers() {
  if (!canAdminFederation.value) return
  fedLoading.value = true
  try {
    const r = await federationApi.listPeers()
    fedPeers.value = (r.data?.items as FederationPeer[]) || []
  } catch (e: any) {
    window.message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    fedLoading.value = false
  }
}

async function submitCreatePeer() {
  const name = (fedCreateModel.name || '').trim()
  if (!name) {
    window.message.error(t('federation.name_required'))
    return
  }
  fedCreating.value = true
  try {
    const r = await federationApi.createPeer(name, fedCreateModel.ttlDays || 0)
    fedCreated.value = r.data as FederationPeerResult
    loadFederationPeers()
  } catch (e: any) {
    window.message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    fedCreating.value = false
  }
}

async function rotateFederationPeer(p: FederationPeer) {
  try {
    const r = await federationApi.rotatePeer(p.id, 90)
    fedCreated.value = r.data as FederationPeerResult
    fedCreateOpen.value = true
    loadFederationPeers()
  } catch (e: any) {
    window.message.error(e?.response?.data?.info || e?.message || String(e))
  }
}

async function revokeFederationPeer(p: FederationPeer) {
  try {
    await federationApi.revokePeer(p.id)
    window.message.success(t('federation.revoked', { name: p.name }))
    loadFederationPeers()
  } catch (e: any) {
    window.message.error(e?.response?.data?.info || e?.message || String(e))
  }
}

function closeCreatePeerModal() {
  fedCreateOpen.value = false
  fedCreated.value = null
  fedCreateModel.name = ''
  fedCreateModel.ttlDays = 90
}

function copyToken(token: string) {
  try {
    navigator.clipboard.writeText(token)
    window.message.success(t('texts.action_success'))
  } catch {
    window.message.error('copy failed')
  }
}

const roleOptions = ref<{ label: string; value: string }[]>([])
const groupRolePairs = ref<{ group: string; role: string }[]>([])
const vaultTesting = ref(false)
const vaultTestMsg = ref('')
const kcTesting = ref(false)
const kcTestResult = ref<Record<string, any> | null>(null)
const kcTestAllOk = computed(() => {
  if (!kcTestResult.value) return false
  return Object.values(kcTestResult.value).every((c: any) => c.ok)
})
const kcImportInput = ref('')
const kcImporting = ref(false)
const kcImportMsg = ref('')
const kcImportType = ref<'success' | 'error' | 'info'>('info')
const vaultTestType = ref<'success' | 'error' | 'warning' | 'info'>('info')

const computedRedirectURI = computed(() => {
  return window.location.origin + '/api/auth/keycloak/callback'
})

function copyRedirect() {
  const uri = computedRedirectURI.value
  try {
    navigator.clipboard.writeText(uri)
    window.message.success(t('texts.action_success'))
  } catch {
    window.message.error('copy failed')
  }
}

async function saveKeycloak() {
  // serialize pairs → map
  const map: Record<string, string> = {}
  for (const p of groupRolePairs.value) {
    if (p.group && p.role) map[p.group] = p.role
  }
  setting.value.keycloak.group_role_map = map
  // ensure the read-only redirect_uri is persisted too
  setting.value.keycloak.redirect_uri = computedRedirectURI.value
  await save('keycloak', setting.value.keycloak)
}

function togglePanel(name: string) {
  if (panel.value === name) {
    panel.value = ''
  } else {
    panel.value = name
  }
}

async function save(id: string, options: any) {
  // Wrap the POST so backend errors (validation, connectivity) land
  // in a readable toast instead of relying on the axios interceptor
  // alone — the interceptor only covers 500s, while our new coded
  // errors travel as 400/422/502.
  try {
    await settingApi.save(id, options)
    window.message.info(t('texts.action_success'));
  } catch (e: any) {
    const info = e?.response?.data?.info || e?.message || String(e)
    window.message.error(info, { duration: 5000 })
  }
}

async function testVault() {
  // Persist current edits first so the backend tests the in-UI values.
  // This mirrors what the Save button would do for the vault section only.
  vaultTesting.value = true
  vaultTestMsg.value = ''
  try {
    await settingApi.save('vault', setting.value.vault)
    const r = await vaultApi.test()
    // r is Result<VaultTestResult> → r.data is the VaultTestResult.
    const res = r.data
    if (res?.ok) {
      vaultTestType.value = 'success'
      vaultTestMsg.value = t('texts.vault_test_ok') + (res.version ? ` (v${res.version})` : '')
    } else {
      vaultTestType.value = 'error'
      vaultTestMsg.value = `[${res?.stage || 'error'}] ${res?.error || t('texts.vault_test_failed')}`
    }
  } catch (e: any) {
    vaultTestType.value = 'error'
    vaultTestMsg.value = e?.message || t('texts.vault_test_failed')
  } finally {
    vaultTesting.value = false
  }
}

async function importKeycloakConfig() {
  const input = kcImportInput.value.trim()
  if (!input) return
  kcImporting.value = true
  kcImportMsg.value = ''
  try {
    let config: any = null

    // Try 1: is it a URL? Fetch the OpenID Configuration JSON
    if (input.startsWith('http://') || input.startsWith('https://')) {
      let url = input
      // If it doesn't end with well-known, append it
      if (!url.includes('.well-known/openid-configuration')) {
        url = url.replace(/\/+$/, '') + '/.well-known/openid-configuration'
      }
      const resp = await fetch(url)
      if (!resp.ok) throw new Error(`Fetch failed: HTTP ${resp.status}`)
      config = await resp.json()
    } else {
      // Try 2: is it a JSON blob pasted directly?
      config = JSON.parse(input)
    }

    if (!config || !config.issuer) {
      throw new Error('No "issuer" field found in the OpenID Configuration. Make sure you copied the OpenID Endpoint Configuration URL or JSON.')
    }

    // Auto-populate fields from the discovered config
    setting.value.keycloak.issuer_url = config.issuer
    setting.value.keycloak.enabled = true
    if (!setting.value.keycloak.scopes) {
      setting.value.keycloak.scopes = 'openid profile email'
    }
    if (!setting.value.keycloak.username_claim) {
      setting.value.keycloak.username_claim = 'preferred_username'
    }
    if (!setting.value.keycloak.email_claim) {
      setting.value.keycloak.email_claim = 'email'
    }
    if (!setting.value.keycloak.groups_claim) {
      setting.value.keycloak.groups_claim = 'groups'
    }
    // Compute redirect_uri from current origin
    setting.value.keycloak.redirect_uri = window.location.origin + '/api/auth/keycloak/callback'

    kcImportType.value = 'success'
    kcImportMsg.value = `Imported from ${config.issuer}. ` +
      `Auth: ${config.authorization_endpoint ? 'OK' : 'missing'}. ` +
      `Token: ${config.token_endpoint ? 'OK' : 'missing'}. ` +
      `You still need to set Client ID and Client Secret manually, then Save.`
  } catch (e: any) {
    kcImportType.value = 'error'
    kcImportMsg.value = e?.message || String(e)
  } finally {
    kcImporting.value = false
  }
}

async function testKeycloak() {
  kcTesting.value = true
  kcTestResult.value = null
  try {
    await settingApi.save('keycloak', setting.value.keycloak)
    // Use native fetch to bypass the global AJAX interceptor that
    // redirects ALL 404s to /404 — the test endpoint may legitimately
    // return 404 if the binary hasn't been rebuilt yet, and we want
    // to show an error message, not a page redirect.
    const headers: Record<string, string> = {}
    if (store.state.user?.token) {
      headers['Authorization'] = 'Bearer ' + store.state.user.token
    }
    const resp = await fetch('/api/setting/keycloak-test', { headers })
    if (!resp.ok) {
      kcTestResult.value = { endpoint: { ok: false, error: `HTTP ${resp.status}: ${resp.statusText}. Rebuild the binary if you just added this feature.` } }
      return
    }
    const body = await resp.json()
    kcTestResult.value = body.data || body || {}
  } catch (e: any) {
    kcTestResult.value = { error: { ok: false, error: e?.message || String(e) } }
  } finally {
    kcTesting.value = false
  }
}

async function fetchData() {
  let r = (await settingApi.load()).data as Setting;
  setting.value = Object.assign(setting.value, r)

  // hydrate keycloak defaults if missing in persisted blob
  if (!setting.value.keycloak) {
    setting.value.keycloak = {
      enabled: false,
      issuer_url: '',
      client_id: '',
      client_secret: '',
      redirect_uri: '',
      scopes: 'openid profile email',
      username_claim: 'preferred_username',
      email_claim: 'email',
      groups_claim: 'groups',
      auto_create_user: false,
      group_role_map: {},
      enable_logout: false,
    }
  }
  const map = setting.value.keycloak.group_role_map || {}
  groupRolePairs.value = Object.keys(map).map(g => ({ group: g, role: map[g] }))

  // hydrate vault defaults if missing in persisted blob
  if (!setting.value.vault) {
    setting.value.vault = {
      enabled: false,
      address: '',
      namespace: '',
      auth_method: 'token',
      token: '',
      approle_path: 'approle',
      role_id: '',
      secret_id: '',
      kv_mount: 'secret',
      kv_prefix: 'swirl/',
      backup_key_path: 'backup-key',
      backup_key_field: 'value',
      default_storage_mode: 'tmpfs',
      tls_skip_verify: false,
      ca_cert: '',
      request_timeout: 10,
    }
  }
  if (!setting.value.backup) {
    setting.value.backup = { storage_mode: 'fs', vault_prefix: 'backups' }
  }
  // Merge defaults with whatever the backend returned so every field
  // the template binds to is guaranteed defined. Object.assign here
  // fills gaps but preserves any saved value. Assigning back as a
  // whole object keeps Vue's reactivity tracking the proxy.
  setting.value.registry_cache = Object.assign(
    rcDefaults(),
    setting.value.registry_cache || {},
  ) as any
  // Coerce just in case a legacy blob ships unexpected values for
  // the enum/numeric fields.
  const rcRm = setting.value.registry_cache.rewrite_mode
  if (rcRm !== 'off' && rcRm !== 'per-host' && rcRm !== 'always') {
    setting.value.registry_cache.rewrite_mode = 'per-host'
  }
  if (typeof setting.value.registry_cache.port !== 'number' || setting.value.registry_cache.port === 0) {
    setting.value.registry_cache.port = 5000
  }
  if (typeof setting.value.registry_cache.use_upstream_prefix !== 'boolean') {
    setting.value.registry_cache.use_upstream_prefix = true
  }

  // load roles for the dropdown
  try {
    const rr = await roleApi.search()
    roleOptions.value = (rr.data || []).map(r => ({ label: r.name, value: r.name }))
  } catch { /* swallow — page still usable */ }
}

// ---------------------------------------------------------------------
// Self-deploy wiring (v3 — flag + sidekick options)
// ---------------------------------------------------------------------
//
// The self-deploy panel is visible to users with `self_deploy.view`.
// Save requires `.edit`. The actual Auto-Deploy button lives on
// compose_stack/Edit.vue — this panel is only Settings.

const canEditSelfDeploy = computed(() => store.getters.allow('self_deploy.edit'))

// Typed reactive copy of the persisted config. Starts with defaults
// so the form is never empty on first mount.
const selfDeploy = ref<SelfDeployConfig>({ ...sdDefaultConfig })

const sdSaving = ref(false)
const sdSaveError = ref('')

// Source-stack dropdown state. Populated at mount from
// /compose-stack/search; the operator picks the Swirl-representing
// stack and the save goes through unchanged.
const sourceStackOptions = ref<{ label: string; value: string }[]>([])
const sdSourceStackLoading = ref(false)

const sdStatus = ref<SelfDeployStatus | null>(null)
const sdResetting = ref(false)
let sdPollTimer: number | null = null

// Progress modal (v3-simplified: spinner + status text, no iframe).
const {
  progressOpen,
  progressStatus,
  progressDescription,
  progressElapsed,
  progressTimedOut,
  progressPhase,
  progressPhaseLabel,
  progressError,
  progressLogTail,
  currentJobId,
  readyStuck,
  resumeFromSession,
  reloadNow,
} = useAutoDeployProgress()

const logTailText = computed(() => {
  if (!sdStatus.value?.logTail || sdStatus.value.logTail.length === 0) return ''
  return sdStatus.value.logTail.slice(-20).join('\n')
})

function phaseTagType(phase: string): 'default' | 'info' | 'success' | 'warning' | 'error' {
  switch (phase) {
    case 'success':
      return 'success'
    case 'failed':
    case 'recovery':
      return 'error'
    case 'rolled_back':
      return 'warning'
    case 'idle':
      return 'default'
    default:
      return 'info'
  }
}

function phaseLabel(phase: string): string {
  const key = `self_deploy.status.${phase}`
  const label = t(key)
  return label === key ? phase : label
}

async function loadSelfDeploy() {
  try {
    const r = await selfDeployApi.loadConfig()
    if (r?.data) {
      const cfg = r.data
      selfDeploy.value = {
        enabled: !!cfg.enabled,
        sourceStackId: cfg.sourceStackId || '',
        autoRollback: cfg.autoRollback ?? true,
        deployTimeout: cfg.deployTimeout || 300,
      }
    }
  } catch (e: any) {
    sdSaveError.value = e?.message || t('self_deploy.errors.save_failed')
  }
}

async function loadSourceStacks() {
  sdSourceStackLoading.value = true
  try {
    const r = await composeStackApi.search({ pageIndex: 1, pageSize: 200 })
    const items: ComposeStackSummary[] = (r?.data?.items as any) || []
    sourceStackOptions.value = items
      .filter(s => !!s.id) // only managed stacks (external have no id)
      .map(s => ({
        label: s.hostName ? `${s.hostName} / ${s.name}` : s.name,
        value: s.id,
      }))
  } catch {
    sourceStackOptions.value = []
  } finally {
    sdSourceStackLoading.value = false
  }
}

async function saveSelfDeploy() {
  sdSaving.value = true
  sdSaveError.value = ''
  try {
    if (selfDeploy.value.enabled && !selfDeploy.value.sourceStackId) {
      sdSaveError.value = t('self_deploy.errors.source_stack_required')
      return
    }
    await selfDeployApi.saveConfig(selfDeploy.value)
    window.message.info(t('texts.action_success'))
  } catch (e: any) {
    sdSaveError.value = e?.response?.data?.info || e?.message || t('self_deploy.errors.save_failed')
  } finally {
    sdSaving.value = false
  }
}

async function resetSelfDeploy() {
  sdResetting.value = true
  try {
    const r = await selfDeployApi.reset()
    if (r?.data?.reclaimed) {
      window.message.success(t('self_deploy.reset.success'))
    } else {
      window.message.info(t('self_deploy.reset.nothing_to_clear'))
    }
    await refreshSelfDeployStatus()
  } catch (e: any) {
    window.message.error(e?.response?.data?.info || e?.message || t('self_deploy.reset.failed'))
  } finally {
    sdResetting.value = false
  }
}

async function refreshSelfDeployStatus() {
  // Skip during an active deploy — Swirl itself is being swapped out,
  // so any /status poll either hangs or is silenced by the interceptor.
  // The composable's fetch-based poll on /api/system/mode already
  // covers "is the new Swirl up yet?" and triggers the modal close on
  // success.
  if (store.state.selfDeployInProgress) return
  try {
    const r = await selfDeployApi.status()
    sdStatus.value = r?.data || null
  } catch {
    /* keep last-known — the panel should stay usable during transient errors */
  }
}

function startSelfDeployPolling() {
  refreshSelfDeployStatus()
  if (sdPollTimer !== null) return
  sdPollTimer = window.setInterval(refreshSelfDeployStatus, 3_000)
}

function stopSelfDeployPolling() {
  if (sdPollTimer !== null) {
    clearInterval(sdPollTimer)
    sdPollTimer = null
  }
}

onMounted(async () => {
  await fetchData()
  await loadSelfDeploy()
  loadSourceStacks()
  loadRegistryOptions()
  startSelfDeployPolling()
  // If we landed here mid-deploy (e.g. the browser reloaded during the
  // sidekick swap), the sessionStorage-backed flag in the store is
  // still set — resume the live progress modal.
  resumeFromSession()
  // Background-load the federation peers list only when the operator
  // has permission AND this instance is the target (MODE=swarm). The
  // standalone portal does not mint peers, so skipping avoids a useless
  // API call on every Settings mount there.
  if (canAdminFederation.value && store.state.mode === 'swarm') {
    loadFederationPeers()
  }
})

onUnmounted(() => {
  stopSelfDeployPolling()
  // The composable registers its own onUnmounted for progress cleanup.
})
</script>

<style scoped>
.toggle {
  width: 75px;
}
.hint {
  margin: -10px 0 12px 12px;
  padding: 8px 12px;
  font-size: 12px;
  color: var(--n-text-color-3, #666);
  background-color: rgba(128, 128, 128, 0.06);
  border-left: 3px solid rgba(64, 128, 255, 0.45);
  border-radius: 4px;
  line-height: 1.55;
}
.hint strong {
  color: var(--n-text-color-2, #444);
  margin-right: 4px;
}
.sd-block-title {
  font-weight: 600;
  margin: 0 0 4px 0;
  font-size: 13px;
}
.sd-hint {
  font-size: 12px;
  color: var(--n-text-color-3, #666);
  margin-bottom: 4px;
  line-height: 1.45;
}
.sd-status-panel {
  border: 1px solid var(--n-border-color, rgba(128, 128, 128, 0.2));
  border-radius: 4px;
  padding: 12px;
  background-color: rgba(128, 128, 128, 0.04);
}
.sd-muted {
  color: var(--n-text-color-3, #888);
  font-size: 12px;
}
.sd-log {
  background-color: rgba(0, 0, 0, 0.04);
  padding: 8px 12px;
  border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  max-height: 240px;
  overflow: auto;
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}
.sd-yaml-input :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
.sd-iframe-fallback {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 24px;
  background: rgba(15, 19, 24, 0.92);
  color: #e4e8ee;
  text-align: center;
  font-size: 14px;
}
.sd-iframe-fallback a {
  color: #4b91ff;
  word-break: break-all;
}
</style>
