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
    <x-panel
      v-if="canViewSelfDeploy"
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
        <!-- Block 1: Enabled flag + disabled-deploy warning -->
        <n-form
          :model="selfDeploy"
          label-placement="left"
          label-width="auto"
        >
          <n-form-item :label="t('self_deploy.enabled')" label-align="right">
            <n-switch v-model:value="selfDeploy.enabled" />
          </n-form-item>
        </n-form>

        <!-- Block: Advanced toggle (show/hide raw template editor) -->
        <div>
          <n-space align="center">
            <n-switch v-model:value="showAdvanced" size="small" />
            <n-text>{{ t('self_deploy.advanced.show_template') }}</n-text>
          </n-space>
          <div class="sd-hint" style="margin-top: 4px;">
            {{ t('self_deploy.advanced.template_hint') }}
          </div>
        </div>

        <!-- Block 2: Compose template (advanced) — hidden by default, kept mounted via v-show -->
        <div v-show="showAdvanced">
          <div class="sd-block-title">{{ t('self_deploy.template') }}</div>
          <div class="sd-hint">{{ t('self_deploy.template_hint') }}</div>
          <x-code-mirror
            v-model="selfDeploy.template"
            :style="{ width: '100%', minHeight: '300px' }"
            height="340px"
          />
        </div>

        <!-- Preview button + output: always visible (useful in basic mode too) -->
        <div>
          <n-space>
            <n-button
              size="small"
              :loading="sdPreviewLoading"
              @click="previewSelfDeploy"
            >{{ t('self_deploy.actions.preview') }}</n-button>
          </n-space>
          <n-alert
            v-if="sdPreviewError"
            type="error"
            :show-icon="true"
            style="margin-top: 8px; font-size: 12px;"
          >{{ sdPreviewError }}</n-alert>
          <div v-if="sdPreviewYaml" style="margin-top: 8px">
            <div class="sd-block-title">{{ t('self_deploy.actions.preview') }}</div>
            <x-code-mirror
              :model-value="sdPreviewYaml"
              :readonly="true"
              :style="{ width: '100%' }"
              height="260px"
            />
          </div>
        </div>

        <!-- Block 3: Placeholders -->
        <div>
          <div class="sd-block-title">{{ t('self_deploy.placeholders.title') }}</div>
          <n-form
            :model="selfDeploy.placeholders"
            label-placement="left"
            label-width="auto"
          >
            <n-form-item :label="t('self_deploy.placeholders.image_tag')" label-align="right">
              <n-input
                v-model:value="selfDeploy.placeholders.imageTag"
                placeholder="cuigh/swirl:latest"
              />
            </n-form-item>
            <n-grid :cols="2" :x-gap="16">
              <n-form-item-gi :label="t('self_deploy.placeholders.expose_port')" label-align="right">
                <n-input-number
                  :min="1"
                  :max="65535"
                  v-model:value="selfDeploy.placeholders.exposePort"
                  style="width: 100%"
                />
              </n-form-item-gi>
              <n-form-item-gi :label="t('self_deploy.placeholders.recovery_port')" label-align="right">
                <n-input-number
                  :min="1"
                  :max="65535"
                  v-model:value="selfDeploy.placeholders.recoveryPort"
                  style="width: 100%"
                />
              </n-form-item-gi>
            </n-grid>
            <n-form-item :label="t('self_deploy.placeholders.recovery_allow')" label-align="right">
              <n-input
                type="textarea"
                :autosize="{ minRows: 2, maxRows: 6 }"
                :placeholder="t('self_deploy.placeholders.recovery_allow_hint')"
                v-model:value="recoveryAllowText"
              />
            </n-form-item>
            <n-alert
              v-if="recoveryAllowWarn"
              type="error"
              :show-icon="true"
              style="margin: -8px 0 12px 0;"
            >{{ t('self_deploy.warnings.allow_any_ip') }}</n-alert>
            <n-form-item :label="t('self_deploy.placeholders.traefik_labels')" label-align="right">
              <n-input
                type="textarea"
                :autosize="{ minRows: 2, maxRows: 8 }"
                :placeholder="t('self_deploy.placeholders.traefik_labels_hint')"
                v-model:value="traefikLabelsText"
              />
            </n-form-item>
            <n-grid :cols="2" :x-gap="16">
              <n-form-item-gi :label="t('self_deploy.placeholders.volume_data')" label-align="right">
                <n-input
                  v-model:value="selfDeploy.placeholders.volumeData"
                  placeholder="swirl_data"
                />
              </n-form-item-gi>
              <n-form-item-gi :label="t('self_deploy.placeholders.network_name')" label-align="right">
                <n-input
                  v-model:value="selfDeploy.placeholders.networkName"
                  placeholder="swirl_net"
                />
              </n-form-item-gi>
            </n-grid>
            <n-form-item :label="t('self_deploy.placeholders.container_name')" label-align="right">
              <n-input
                v-model:value="selfDeploy.placeholders.containerName"
                placeholder="swirl"
              />
            </n-form-item>
            <n-form-item :label="t('self_deploy.placeholders.extra_env')" label-align="right">
              <n-input
                type="textarea"
                :autosize="{ minRows: 2, maxRows: 10 }"
                :placeholder="t('self_deploy.placeholders.extra_env_hint')"
                v-model:value="extraEnvText"
              />
            </n-form-item>
          </n-form>
        </div>

        <!-- Block 4: Advanced -->
        <div>
          <div class="sd-block-title">{{ t('self_deploy.advanced.title') }}</div>
          <n-form
            :model="selfDeploy"
            label-placement="left"
            label-width="auto"
          >
            <n-form-item :label="t('self_deploy.advanced.auto_rollback')" label-align="right">
              <n-switch v-model:value="selfDeploy.autoRollback" />
            </n-form-item>
            <n-form-item :label="t('self_deploy.advanced.deploy_timeout')" label-align="right">
              <n-input-number
                :min="60"
                :max="1800"
                v-model:value="selfDeploy.deployTimeout"
                style="width: 100%"
              />
            </n-form-item>
          </n-form>
        </div>

        <!-- Block 5: Actions + Status -->
        <n-space>
          <n-button
            v-if="canEditSelfDeploy"
            type="primary"
            :loading="sdSaving"
            @click="saveSelfDeploy"
          >{{ t('self_deploy.actions.save') }}</n-button>
          <n-button
            v-if="canExecuteSelfDeploy"
            type="error"
            :loading="sdDeploying"
            :disabled="!selfDeploy.enabled"
            @click="openDeployConfirm"
          >{{ t('self_deploy.actions.deploy') }}</n-button>
        </n-space>
        <n-alert
          v-if="!selfDeploy.enabled && canExecuteSelfDeploy"
          type="warning"
          :show-icon="true"
        >{{ t('self_deploy.warnings.disabled_cannot_deploy') }}</n-alert>

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
            v-if="sdStatus.recoveryActive"
            type="error"
            :show-icon="true"
            :title="t('self_deploy.status.recovery')"
            style="margin-bottom: 8px"
          >
            <a
              v-if="recoveryLink"
              :href="recoveryLink"
              target="_blank"
              rel="noopener"
            >{{ t('self_deploy.status.recovery_url') }}: {{ recoveryLink }}</a>
          </n-alert>
          <n-alert
            v-if="sdStatus.error"
            type="error"
            :show-icon="true"
            style="margin-bottom: 8px"
          >{{ sdStatus.error }}</n-alert>
          <div class="sd-block-title">{{ t('self_deploy.status.log_tail') }}</div>
          <pre class="sd-log">{{ logTailText || t('self_deploy.status.no_logs') }}</pre>
        </div>
        <n-alert
          v-if="reconnectFailed"
          type="warning"
          :show-icon="true"
        >{{ t('self_deploy.status.reconnect_failed') }}</n-alert>
      </n-space>
    </x-panel>

    <!-- Deploy confirmation modal -->
    <n-modal
      v-model:show="showDeployConfirm"
      preset="dialog"
      type="error"
      :title="t('self_deploy.actions.confirm_deploy_title')"
      :positive-text="t('self_deploy.actions.deploy')"
      :negative-text="t('buttons.cancel')"
      :positive-button-props="{ disabled: !deployAck, loading: sdDeploying }"
      @positive-click="confirmDeploy"
      @negative-click="showDeployConfirm = false"
    >
      <div style="margin-bottom: 12px;">
        {{ t('self_deploy.actions.confirm_deploy_body') }}
      </div>
      <n-checkbox v-model:checked="deployAck">
        {{ t('self_deploy.actions.confirm_ack') }}
      </n-checkbox>
    </n-modal>

    <!-- Live progress modal: iframes the sidekick UI while the deploy runs. -->
    <n-modal
      v-model:show="progressOpen"
      :mask-closable="false"
      :closable="false"
      preset="card"
      :bordered="false"
      style="width: 80vw; height: 80vh; max-width: 1200px;"
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
      <div style="position: relative; width: 100%; height: calc(80vh - 90px);">
        <iframe
          v-if="progressUrl"
          ref="progressIframe"
          :src="progressUrl"
          style="width: 100%; height: 100%; border: 0; background: #0f1318;"
          @load="onIframeLoad"
          @error="onIframeError"
        />
        <div
          v-if="progressIframeFailed"
          class="sd-iframe-fallback"
        >
          <p>
            {{ iframeFallbackMessage }}
          </p>
          <p v-if="progressUrl">
            <a :href="progressUrl" target="_blank" rel="noopener">{{ progressUrl }}</a>
          </p>
        </div>
      </div>
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
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
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
  NCheckbox,
  NModal,
  NSpin,
} from "naive-ui";
import XPageHeader from "@/components/PageHeader.vue";
import XPanel from "@/components/Panel.vue";
import XCodeMirror from "@/components/CodeMirror.vue";
import settingApi from "@/api/setting";
import { store } from "@/store";
import type { Setting } from "@/api/setting";
import vaultApi from "@/api/vault";
import roleApi from "@/api/role";
import selfDeployApi, {
  defaultConfig as sdDefaultConfig,
  defaultPlaceholders as sdDefaultPlaceholders,
  type SelfDeployConfig,
  type SelfDeployStatus,
} from "@/api/self-deploy";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
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
} as Setting);
const panel = ref('')
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
  await settingApi.save(id, options)
  window.message.info(t('texts.action_success'));
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

  // load roles for the dropdown
  try {
    const rr = await roleApi.search()
    roleOptions.value = (rr.data || []).map(r => ({ label: r.name, value: r.name }))
  } catch { /* swallow — page still usable */ }
}

// ---------------------------------------------------------------------
// Self-deploy wiring
// ---------------------------------------------------------------------
//
// The self-deploy panel is visible to users with `self_deploy.view`.
// Save requires `.edit`, Deploy requires `.execute`. Gating happens
// both in the template (v-if) and at the API layer (auth tag).

const canViewSelfDeploy = computed(() => store.getters.allow('self_deploy.view'))
const canEditSelfDeploy = computed(() => store.getters.allow('self_deploy.edit'))
const canExecuteSelfDeploy = computed(() => store.getters.allow('self_deploy.execute'))

// Typed reactive copy of the persisted config. Starts with planning
// defaults so the form is never empty on first mount.
const selfDeploy = ref<SelfDeployConfig>({
  ...sdDefaultConfig,
  placeholders: { ...sdDefaultPlaceholders },
})

// Per-field UI state. The CIDR / label / env textareas are plain
// strings that we split/join around the backend roundtrip — this keeps
// the form ergonomic (operators paste a block of lines) without
// pushing the serialization concern into the backend contract.
const recoveryAllowText = ref('')
const traefikLabelsText = ref('')
const extraEnvText = ref('')

const sdPreviewYaml = ref('')
const sdPreviewError = ref('')
const sdPreviewLoading = ref(false)
const sdSaving = ref(false)
const sdDeploying = ref(false)
const sdSaveError = ref('')
const showAdvanced = ref(false)

const sdStatus = ref<SelfDeployStatus | null>(null)
let sdPollTimer: number | null = null

const showDeployConfirm = ref(false)
const deployAck = ref(false)
const reconnectFailed = ref(false)
const lastRecoveryPort = ref<number>(0)

// Live-progress iframe modal state. The iframe points at the sidekick
// HTTP server (same machinery that used to only spawn on failure;
// Phase 8 made it always-on). The modal opens the moment the operator
// confirms a deploy and closes either when (a) /api/system/mode returns
// 200 on the new Swirl, or (b) the sidekick posts a "success" message
// to window.parent.
const progressOpen = ref(false)
const progressUrl = ref('')
const progressIframe = ref<HTMLIFrameElement | null>(null)
const progressIframeFailed = ref(false)
const progressIframeLoaded = ref(false)
const progressTimedOut = ref(false)
let progressPostMsgHandler: ((ev: MessageEvent) => void) | null = null
let progressPollTimer: number | null = null
let progressTimeoutTimer: number | null = null
let progressLoadGuardTimer: number | null = null

const iframeFallbackMessage = computed(() =>
  t('self_deploy.progress.failed_to_connect', { url: progressUrl.value || '' })
)

// A block of CIDRs where any entry equals 0.0.0.0/0 triggers a red
// banner — matches the same rule enforced by the biz validator.
const recoveryAllowWarn = computed(() => {
  return (selfDeploy.value.placeholders.recoveryAllow || []).some(
    (c: string) => c.trim() === '0.0.0.0/0'
  )
})

const recoveryLink = computed(() => {
  const port = lastRecoveryPort.value || selfDeploy.value.placeholders.recoveryPort
  if (!port) return ''
  // The sidekick binds 127.0.0.1 by default — use the current origin's
  // scheme/host so operators on the same machine get a clickable link.
  return `${window.location.protocol}//${window.location.hostname}:${port}/`
})

const logTailText = computed(() => {
  if (!sdStatus.value?.logTail || sdStatus.value.logTail.length === 0) return ''
  return sdStatus.value.logTail.slice(-20).join('\n')
})

// Keep selfDeploy.placeholders.recoveryAllow in sync with the textarea.
watch(recoveryAllowText, (v) => {
  selfDeploy.value.placeholders.recoveryAllow = splitLines(v)
})
watch(traefikLabelsText, (v) => {
  selfDeploy.value.placeholders.traefikLabels = splitLines(v)
})
watch(extraEnvText, (v) => {
  selfDeploy.value.placeholders.extraEnv = parseEnvLines(v)
})

function splitLines(v: string): string[] {
  return (v || '')
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
}

function parseEnvLines(v: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of splitLines(v)) {
    const idx = line.indexOf('=')
    if (idx <= 0) continue
    const key = line.slice(0, idx).trim()
    if (!key) continue
    out[key] = line.slice(idx + 1)
  }
  return out
}

function serializeEnv(env: Record<string, string> | undefined | null): string {
  if (!env) return ''
  return Object.keys(env)
    .sort()
    .map((k) => `${k}=${env[k]}`)
    .join('\n')
}

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
  // Fall back to the raw phase if we don't have a translation — keeps
  // the UI readable for phases added in later phases (Fase 6+).
  const label = t(key)
  return label === key ? phase : label
}

async function loadSelfDeploy() {
  if (!canViewSelfDeploy.value) return
  try {
    const r = await selfDeployApi.loadConfig()
    if (r?.data) {
      const cfg = r.data
      // Merge with defaults to protect against missing fields in older
      // persisted blobs (no schema migration — see the Settings note in
      // CLAUDE.md).
      selfDeploy.value = {
        enabled: !!cfg.enabled,
        template: cfg.template || '',
        autoRollback: cfg.autoRollback ?? true,
        deployTimeout: cfg.deployTimeout || 300,
        placeholders: {
          ...sdDefaultPlaceholders,
          ...(cfg.placeholders || {}),
        },
      }
      recoveryAllowText.value = (selfDeploy.value.placeholders.recoveryAllow || []).join('\n')
      traefikLabelsText.value = (selfDeploy.value.placeholders.traefikLabels || []).join('\n')
      extraEnvText.value = serializeEnv(selfDeploy.value.placeholders.extraEnv)
    }
  } catch (e: any) {
    sdSaveError.value = e?.message || t('self_deploy.errors.save_failed')
  }
}

async function previewSelfDeploy() {
  sdPreviewLoading.value = true
  sdPreviewError.value = ''
  sdPreviewYaml.value = ''
  try {
    // Pass the current placeholders so the preview reflects unsaved
    // edits. The backend still renders against the *saved* template
    // (we don't override it here) — that's documented in the API
    // handler comment.
    const r = await selfDeployApi.preview(selfDeploy.value.placeholders)
    sdPreviewYaml.value = r?.data?.yaml || ''
  } catch (e: any) {
    sdPreviewError.value = e?.response?.data?.info || e?.message || t('self_deploy.errors.preview_failed')
  } finally {
    sdPreviewLoading.value = false
  }
}

async function saveSelfDeploy() {
  sdSaving.value = true
  sdSaveError.value = ''
  try {
    // Sync textareas one last time in case the watch is still debouncing.
    selfDeploy.value.placeholders.recoveryAllow = splitLines(recoveryAllowText.value)
    selfDeploy.value.placeholders.traefikLabels = splitLines(traefikLabelsText.value)
    selfDeploy.value.placeholders.extraEnv = parseEnvLines(extraEnvText.value)
    await selfDeployApi.saveConfig(selfDeploy.value)
    window.message.info(t('texts.action_success'))
  } catch (e: any) {
    sdSaveError.value = e?.response?.data?.info || e?.message || t('self_deploy.errors.save_failed')
  } finally {
    sdSaving.value = false
  }
}

function openDeployConfirm() {
  deployAck.value = false
  showDeployConfirm.value = true
}

async function confirmDeploy() {
  if (!deployAck.value) return
  sdDeploying.value = true
  try {
    const r = await selfDeployApi.deploy()
    showDeployConfirm.value = false
    const rawUrl = r?.data?.recoveryUrl || ''
    if (rawUrl) {
      const portMatch = rawUrl.match(/:(\d+)$/)
      if (portMatch) lastRecoveryPort.value = parseInt(portMatch[1], 10)
    }
    if (!lastRecoveryPort.value && selfDeploy.value.placeholders.recoveryPort) {
      lastRecoveryPort.value = selfDeploy.value.placeholders.recoveryPort
    }
    // Resolve the iframe URL. The backend ships either a bare port
    // (":8002") or a fully-qualified URL. Prefix with scheme + hostname
    // of the current Swirl origin so an operator hitting Swirl through
    // a reverse proxy still gets a sidekick URL they can reach.
    progressUrl.value = buildProgressUrl(rawUrl, lastRecoveryPort.value)
    progressIframeFailed.value = false
    progressIframeLoaded.value = false
    progressTimedOut.value = false
    progressOpen.value = true
    reconnectFailed.value = false

    startProgressPolling()
    addProgressPostMessageListener()
    startProgressLoadGuard()
    startProgressTimeoutGuard()
  } catch (e: any) {
    sdSaveError.value = e?.response?.data?.info || e?.message || t('self_deploy.errors.deploy_failed')
  } finally {
    sdDeploying.value = false
  }
}

// buildProgressUrl composes an iframe-loadable URL from whatever the
// backend hands us. Three acceptable input shapes:
//   - "" (empty)             → fallback: current origin + ":<port>" if known
//   - ":8002"                → prefix with scheme + hostname of current origin
//   - "http(s)://host:port/" → use verbatim
function buildProgressUrl(raw: string, portHint: number): string {
  const origin = window.location
  if (!raw) {
    if (!portHint) return ''
    return `${origin.protocol}//${origin.hostname}:${portHint}/`
  }
  if (/^https?:\/\//i.test(raw)) return raw
  if (raw.startsWith(':')) {
    return `${origin.protocol}//${origin.hostname}${raw}/`
  }
  // Bare port or something else — best-effort compose.
  return `${origin.protocol}//${origin.hostname}:${raw}/`
}

// startProgressPolling runs the /api/system/mode polling in parallel
// with the iframe so the modal closes the instant the new Swirl is up,
// without relying solely on postMessage (which would need the iframe to
// be loadable — not guaranteed for allow-list blocks).
function startProgressPolling() {
  stopProgressPolling()
  const tick = async () => {
    if (!progressOpen.value) return
    try {
      const resp = await fetch('/api/system/mode', { cache: 'no-store' })
      if (resp.ok) {
        onDeploySuccess()
        return
      }
    } catch {
      /* still down — the new container is not yet serving */
    }
  }
  // First probe immediately (cheap; non-blocking on failure), then
  // every 3s while the modal is open.
  tick()
  progressPollTimer = window.setInterval(tick, 3000)
}

function stopProgressPolling() {
  if (progressPollTimer !== null) {
    clearInterval(progressPollTimer)
    progressPollTimer = null
  }
}

// addProgressPostMessageListener wires the sidekick's postMessage
// (see cmd/deploy_agent/ui/script.js::togglePanels) so a successful
// deploy closes the modal without waiting for the polling loop.
function addProgressPostMessageListener() {
  removeProgressPostMessageListener()
  progressPostMsgHandler = (ev: MessageEvent) => {
    const d = ev.data
    if (d && typeof d === 'object' && d.type === 'swirl.self-deploy' && d.phase === 'success') {
      onDeploySuccess()
    }
  }
  window.addEventListener('message', progressPostMsgHandler)
}

function removeProgressPostMessageListener() {
  if (progressPostMsgHandler) {
    window.removeEventListener('message', progressPostMsgHandler)
    progressPostMsgHandler = null
  }
}

// startProgressLoadGuard: if the iframe has not dispatched a `load`
// event within 10s, show a textual fallback with the URL so the
// operator can open it in a new tab. Does NOT close the modal — the
// deploy is still running in the background.
function startProgressLoadGuard() {
  if (progressLoadGuardTimer !== null) {
    clearTimeout(progressLoadGuardTimer)
  }
  progressLoadGuardTimer = window.setTimeout(() => {
    if (!progressIframeLoaded.value) {
      progressIframeFailed.value = true
    }
  }, 10_000)
}

// startProgressTimeoutGuard: after 5 minutes without either a
// /api/system/mode success or a postMessage, show the "taking longer
// than expected" warning in the modal header. The modal stays open —
// the operator can still interact with the iframe.
function startProgressTimeoutGuard() {
  if (progressTimeoutTimer !== null) {
    clearTimeout(progressTimeoutTimer)
  }
  progressTimeoutTimer = window.setTimeout(() => {
    progressTimedOut.value = true
  }, 5 * 60 * 1000)
}

function onIframeLoad() {
  progressIframeLoaded.value = true
  progressIframeFailed.value = false
}

function onIframeError() {
  progressIframeFailed.value = true
}

function onDeploySuccess() {
  stopProgressPolling()
  removeProgressPostMessageListener()
  if (progressTimeoutTimer !== null) {
    clearTimeout(progressTimeoutTimer)
    progressTimeoutTimer = null
  }
  if (progressLoadGuardTimer !== null) {
    clearTimeout(progressLoadGuardTimer)
    progressLoadGuardTimer = null
  }
  progressOpen.value = false
  // Full reload so Vuex and the live setting snapshot are fresh.
  window.location.assign('/')
}

async function refreshSelfDeployStatus() {
  if (!canViewSelfDeploy.value) return
  try {
    const r = await selfDeployApi.status()
    sdStatus.value = r?.data || null
    if (sdStatus.value?.recoveryActive && !lastRecoveryPort.value) {
      lastRecoveryPort.value = selfDeploy.value.placeholders.recoveryPort
    }
  } catch {
    /* keep last-known — the panel should stay usable during transient errors */
  }
}

function startSelfDeployPolling() {
  if (!canViewSelfDeploy.value) return
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
  startSelfDeployPolling()
})

onUnmounted(() => {
  stopSelfDeployPolling()
  stopProgressPolling()
  removeProgressPostMessageListener()
  if (progressTimeoutTimer !== null) {
    clearTimeout(progressTimeoutTimer)
    progressTimeoutTimer = null
  }
  if (progressLoadGuardTimer !== null) {
    clearTimeout(progressLoadGuardTimer)
    progressLoadGuardTimer = null
  }
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
  margin: 0 0 6px 0;
  font-size: 14px;
}
.sd-hint {
  font-size: 12px;
  color: var(--n-text-color-3, #666);
  margin-bottom: 6px;
  line-height: 1.5;
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
