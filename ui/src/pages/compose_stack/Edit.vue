<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="onReturn">
        <template #icon>
          <n-icon>
            <arrow-back-icon />
          </n-icon>
        </template>
        {{ t('buttons.return') }}
      </n-button>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="16">
    <n-form :model="model" ref="form" :rules="rules" label-placement="top">
      <n-grid cols="2" x-gap="16">
        <n-form-item-gi :label="t('objects.host')" path="hostId">
          <n-select
            filterable
            :options="hosts"
            v-model:value="model.hostId"
            :disabled="isEdit"
            :placeholder="t('objects.host')"
          />
        </n-form-item-gi>
        <n-form-item-gi :label="t('fields.name')" path="name">
          <n-input v-model:value="model.name" :disabled="isEdit" :placeholder="t('fields.name')" />
        </n-form-item-gi>
      </n-grid>
      <n-form-item :label="t('fields.content')" path="content">
        <x-code-mirror
          v-model="model.content"
          :style="{ width: '100%', height: '55vh' }"
        />
      </n-form-item>
      <n-space>
        <n-checkbox v-model:checked="pullImages">{{ t('fields.pull_images') || 'Pull images' }}</n-checkbox>
      </n-space>
    </n-form>

    <!-- Env file (.env-style variables) — substituted into the compose YAML
         at deploy time via ${VAR} expansion. Persisted alongside the stack. -->
    <x-panel
      :title="t('stack_secret.env_file_title')"
      :subtitle="t('stack_secret.env_file_subtitle')"
      divider="bottom"
    >
      <n-input
        type="textarea"
        v-model:value="model.envFile"
        :placeholder="t('stack_secret.env_file_placeholder')"
        :autosize="{ minRows: 3, maxRows: 15 }"
        :input-props="{ style: 'font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px;' }"
      />
    </x-panel>

    <n-space>
      <n-button
        :type="isSelfDeployStack ? 'warning' : 'primary'"
        :loading="submitting"
        @click="isSelfDeployStack ? autoDeployStack() : deployStack()"
      >
        <template #icon>
          <n-icon><rocket-outline /></n-icon>
        </template>
        {{ isSelfDeployStack ? t('self_deploy.actions.auto_deploy') : t('buttons.deploy') }}
      </n-button>
      <n-button secondary :loading="submitting" @click="saveStack">
        <template #icon>
          <n-icon><save-outline /></n-icon>
        </template>
        {{ t('buttons.save') }}
      </n-button>
    </n-space>

    <!-- Auto-Deploy confirmation dialog -->
    <n-modal
      v-model:show="autoDeployConfirm"
      preset="dialog"
      type="warning"
      :title="t('self_deploy.actions.auto_deploy_confirm_title')"
      :positive-text="t('self_deploy.actions.auto_deploy')"
      :negative-text="t('buttons.cancel')"
      :positive-button-props="{ loading: submitting }"
      @positive-click="confirmAutoDeploy"
      @negative-click="autoDeployConfirm = false"
    >
      <div>{{ t('self_deploy.actions.auto_deploy_confirm_body') }}</div>
    </n-modal>

    <!-- Live progress modal (shared composable) -->
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
        <div v-if="progressIframeFailed" class="ads-iframe-fallback">
          <p>{{ iframeFallbackMessage }}</p>
          <p v-if="progressUrl">
            <a :href="progressUrl" target="_blank" rel="noopener">{{ progressUrl }}</a>
          </p>
        </div>
      </div>
    </n-modal>

    <!--
      Bindings panel: lets the operator attach VaultSecret entries to this
      stack. Only visible after the stack has an id, because each binding is
      keyed by stackId. The actual values are resolved from Vault at deploy
      time — Swirl never persists them.
    -->
    <x-panel
      v-if="isEdit"
      :title="t('stack_secret.title')"
      :subtitle="t('stack_secret.subtitle')"
      divider="bottom"
    >
      <template #action>
        <n-space :size="8">
          <n-button secondary size="small" @click="reloadBindings" :loading="bindingsLoading">
            <template #icon>
              <n-icon><refresh-icon /></n-icon>
            </template>
            {{ t('buttons.refresh') }}
          </n-button>
          <n-button
            type="primary"
            secondary
            size="small"
            :disabled="!vaultSecrets.length"
            @click="openWizard()"
          >
            <template #icon>
              <n-icon><add-icon /></n-icon>
            </template>
            {{ t('buttons.add') }}
          </n-button>
        </n-space>
      </template>

      <n-alert
        v-if="!vaultSecrets.length"
        type="info"
        :show-icon="false"
        style="margin-bottom: 12px;"
      >
        {{ t('stack_secret.no_secrets_hint') }}
      </n-alert>

      <n-table size="small" :bordered="true" :single-line="false">
        <thead>
          <tr>
            <th>{{ t('objects.vault_secret') }}</th>
            <th>{{ t('fields.field') }}</th>
            <th>{{ t('objects.service') }}</th>
            <th>{{ t('stack_secret.target_type') }}</th>
            <th>{{ t('stack_secret.target') }}</th>
            <th>{{ t('stack_secret.storage') }}</th>
            <th>{{ t('stack_secret.deployed') }}</th>
            <th>{{ t('fields.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(b, index) of bindings" :key="b.id">
            <td>
              <code>{{ vaultSecretName(b.vaultSecretId) }}</code>
            </td>
            <td>
              <code v-if="b.field">{{ b.field }}</code>
              <span v-else class="muted">auto</span>
            </td>
            <td>
              <span v-if="b.service">{{ b.service }}</span>
              <span v-else class="muted">{{ t('stack_secret.all_services') }}</span>
            </td>
            <td>
              <n-tag size="small" :type="b.targetType === 'env' ? 'warning' : 'info'">
                {{ b.targetType }}
              </n-tag>
            </td>
            <td>
              <code v-if="b.targetType === 'file'">{{ b.targetPath }}</code>
              <code v-else>${{ b.envName }}</code>
            </td>
            <td>
              <span v-if="b.targetType === 'file'">{{ b.storageMode || 'tmpfs' }}</span>
              <span v-else class="muted">—</span>
            </td>
            <td>
              <n-space v-if="b.deployedAt" :size="4" inline>
                <n-tag size="tiny" type="success" round>
                  <n-time :time="b.deployedAt" format="y-MM-dd HH:mm" />
                </n-tag>
                <n-tooltip v-if="b.deployedHash">
                  <template #trigger>
                    <code class="hash">{{ shortHash(b.deployedHash) }}</code>
                  </template>
                  {{ b.deployedHash }}
                </n-tooltip>
                <n-tooltip v-if="driftBadge(b.id)">
                  <template #trigger>
                    <n-tag
                      size="tiny"
                      round
                      :type="driftBadge(b.id)!.type as any"
                    >{{ driftBadge(b.id)!.label }}</n-tag>
                  </template>
                  {{ driftMap[b.id]?.message || driftBadge(b.id)!.label }}
                </n-tooltip>
              </n-space>
              <span v-else class="muted">{{ t('stack_secret.not_deployed') }}</span>
            </td>
            <td>
              <n-button
                size="tiny"
                quaternary
                type="warning"
                @click="openBindingModal(b)"
              >{{ t('buttons.edit') }}</n-button>
              <n-popconfirm :show-icon="false" @positive-click="deleteBinding(b.id, index)">
                <template #trigger>
                  <n-button size="tiny" quaternary type="error">{{ t('buttons.delete') }}</n-button>
                </template>
                {{ t('prompts.delete') }}
              </n-popconfirm>
            </td>
          </tr>
          <tr v-if="!bindings.length">
            <td colspan="8" style="text-align: center; padding: 16px;">
              <span class="muted">{{ t('stack_secret.empty') }}</span>
            </td>
          </tr>
        </tbody>
      </n-table>
    </x-panel>

    <n-modal
      v-model:show="bindingModalOpen"
      preset="card"
      :title="bindingForm.id ? t('stack_secret.edit_title') : t('stack_secret.new_title')"
      style="width: 640px;"
      :mask-closable="false"
    >
      <n-form :model="bindingForm" :rules="bindingRules" ref="bindingFormRef" label-placement="top">
        <n-form-item :label="t('objects.vault_secret')" path="vaultSecretId">
          <n-select
            filterable
            v-model:value="bindingForm.vaultSecretId"
            :options="vaultSecretOptions"
            :placeholder="t('objects.vault_secret')"
          />
        </n-form-item>
        <n-form-item :label="t('fields.field')" path="field">
          <n-input
            v-model:value="bindingForm.field"
            placeholder="e.g. DB_PASSWORD (leave empty for auto)"
          />
        </n-form-item>
        <n-form-item :label="t('objects.service')" path="service">
          <n-input
            v-model:value="bindingForm.service"
            :placeholder="t('stack_secret.service_placeholder')"
          />
        </n-form-item>
        <n-form-item :label="t('stack_secret.target_type')" path="targetType">
          <n-radio-group v-model:value="bindingForm.targetType">
            <n-radio value="file">{{ t('stack_secret.target_file') }}</n-radio>
            <n-radio value="env">{{ t('stack_secret.target_env') }}</n-radio>
          </n-radio-group>
        </n-form-item>
        <n-form-item
          v-if="bindingForm.targetType === 'file'"
          :label="t('fields.target_path')"
          path="targetPath"
        >
          <n-input
            v-model:value="bindingForm.targetPath"
            placeholder="/run/secrets/db_password"
          />
        </n-form-item>
        <n-form-item
          v-if="bindingForm.targetType === 'env'"
          :label="t('stack_secret.env_name')"
          path="envName"
        >
          <n-input
            v-model:value="bindingForm.envName"
            placeholder="DB_PASSWORD"
          />
        </n-form-item>
        <template v-if="bindingForm.targetType === 'file'">
          <n-form-item :label="t('stack_secret.storage_mode')" path="storageMode">
            <n-select
              v-model:value="bindingForm.storageMode"
              :options="storageOptions"
            />
          </n-form-item>
          <n-grid cols="3" x-gap="12">
            <n-form-item-gi :label="t('stack_secret.uid')" path="uid">
              <n-input-number v-model:value="bindingForm.uid" :min="0" />
            </n-form-item-gi>
            <n-form-item-gi :label="t('stack_secret.gid')" path="gid">
              <n-input-number v-model:value="bindingForm.gid" :min="0" />
            </n-form-item-gi>
            <n-form-item-gi :label="t('stack_secret.file_mode')" path="mode">
              <n-input v-model:value="bindingForm.mode" placeholder="0400" />
            </n-form-item-gi>
          </n-grid>
        </template>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="bindingModalOpen = false">{{ t('buttons.cancel') }}</n-button>
          <n-button type="primary" :loading="savingBinding" @click="saveBinding">
            {{ t('buttons.save') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>

    <!--
      Wizard modal for adding MULTIPLE secret bindings at once.
      Step 1: pick VaultSecret
      Step 2: pick fields from the secret (via Preview)
      Step 3: configure each field (service, env var name, target type)
    -->
    <n-modal
      v-model:show="wizardOpen"
      preset="card"
      :title="t('stack_secret.wizard_title')"
      style="width: 720px;"
      :mask-closable="false"
    >
      <!-- Step 1: select secret -->
      <div v-if="wizardStep === 1">
        <n-form-item :label="t('stack_secret.select_secret')">
          <n-select
            filterable
            v-model:value="wizardSecretId"
            :options="vaultSecretOptions"
            :placeholder="t('objects.vault_secret')"
            @update:value="wizardLoadFields"
          />
        </n-form-item>
        <n-spin v-if="wizardLoadingFields" size="small" />
        <n-alert v-if="wizardFields.length === 0 && wizardSecretId && !wizardLoadingFields" type="warning">
          {{ t('stack_secret.no_fields_found') }}
        </n-alert>
      </div>

      <!-- Step 2: pick fields -->
      <div v-else-if="wizardStep === 2">
        <div style="margin-bottom: 12px; opacity: 0.7;">{{ t('stack_secret.pick_fields_hint') }}</div>
        <n-checkbox-group v-model:value="wizardSelectedFields">
          <n-space vertical :size="8">
            <n-checkbox v-for="f of wizardFields" :key="f" :value="f" :label="f" />
          </n-space>
        </n-checkbox-group>
      </div>

      <!-- Step 3: configure each -->
      <div v-else-if="wizardStep === 3">
        <div style="margin-bottom: 12px; opacity: 0.7;">{{ t('stack_secret.configure_bindings') }}</div>
        <n-table size="small" :bordered="true" :single-line="false">
          <thead>
            <tr>
              <th>{{ t('fields.field') }}</th>
              <th>{{ t('objects.service') }}</th>
              <th>{{ t('stack_secret.env_name') }}</th>
              <th>{{ t('stack_secret.target_type') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(cfg, i) of wizardConfig" :key="cfg.field">
              <td><code>{{ cfg.field }}</code></td>
              <td>
                <n-select
                  size="small"
                  v-model:value="cfg.service"
                  :options="serviceOptions"
                  style="min-width: 140px;"
                />
              </td>
              <td>
                <n-input size="small" v-model:value="cfg.envName" />
              </td>
              <td>
                <n-radio-group size="small" v-model:value="cfg.targetType">
                  <n-radio value="env">env</n-radio>
                  <n-radio value="file">file</n-radio>
                </n-radio-group>
              </td>
            </tr>
          </tbody>
        </n-table>
      </div>

      <template #footer>
        <n-space justify="end">
          <n-button @click="wizardOpen = false">{{ t('buttons.cancel') }}</n-button>
          <n-button
            v-if="wizardStep > 1"
            @click="wizardStep--"
          >{{ t('buttons.prev') }}</n-button>
          <n-button
            v-if="wizardStep < 3"
            type="primary"
            :disabled="!wizardCanNext"
            @click="wizardNext"
          >{{ t('buttons.next') }}</n-button>
          <n-button
            v-if="wizardStep === 3"
            type="primary"
            :loading="wizardSaving"
            @click="wizardSubmit"
          >{{ t('stack_secret.add_n_bindings', { n: wizardConfig.length }) }}</n-button>
        </n-space>
      </template>
    </n-modal>
  </n-space>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import {
  NSpace, NButton, NForm, NFormItem, NFormItemGi, NGrid, NInput, NInputNumber,
  NSelect, NCheckbox, NCheckboxGroup, NIcon, NTable, NTag, NPopconfirm, NTime, NTooltip, NSpin,
  NModal, NRadio, NRadioGroup, NAlert,
  useMessage,
} from "naive-ui";
import {
  ArrowBackCircleOutline as ArrowBackIcon,
  RocketOutline, SaveOutline,
  AddOutline as AddIcon,
  RefreshOutline as RefreshIcon,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XPanel from "@/components/Panel.vue";
import XCodeMirror from "@/components/CodeMirror.vue";
import composeStackApi from "@/api/compose_stack";
import type { ComposeStack } from "@/api/compose_stack";
import composeStackSecretApi from "@/api/compose-stack-secret";
import type {
  ComposeStackSecretBinding,
  ComposeStackSecretDrift,
} from "@/api/compose-stack-secret";
import vaultSecretApi from "@/api/vault-secret";
import type { VaultSecret } from "@/api/vault-secret";
import * as hostApi from "@/api/host";
import selfDeployApi, { type SelfDeployConfig } from "@/api/self-deploy";
import { useAutoDeployProgress } from "@/composables/useAutoDeployProgress";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'
import { requiredRule } from "@/utils/form";
import { returnTo } from "@/utils/nav";

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const message = useMessage()
const form = ref()
const submitting = ref(false)
const pullImages = ref(false)
const isEdit = computed(() => !!route.params.id)

function onReturn() {
  // When editing an existing stack and there's no history (deep link), go to
  // the stack's detail page rather than the list — the detail page is the
  // "parent" context for the editor.
  if (isEdit.value && route.params.id) {
    returnTo({ name: 'std_stack_detail', params: { id: route.params.id as string } })
  } else {
    returnTo({ name: 'std_stack_list' })
  }
}

const model = reactive({
  id: '',
  hostId: '',
  name: '',
  content: '',
  envFile: '',
  errorMessage: '',
} as ComposeStack)

const hosts: any = ref([])
const rules = {
  hostId: requiredRule(),
  name: requiredRule(),
  content: requiredRule(),
}

async function validate(): Promise<boolean> {
  try {
    await (form.value as any).validate()
    return true
  } catch {
    return false
  }
}

async function saveStack() {
  if (!await validate()) return
  submitting.value = true
  try {
    const r = await composeStackApi.save(model)
    message.success(t('buttons.save'))
    router.replace({ name: 'std_stack_edit', params: { id: r.data?.id || model.id } })
  } catch (e: any) {
    message.error(e?.message || String(e))
  } finally {
    submitting.value = false
  }
}

async function deployStack() {
  if (!await validate()) return
  submitting.value = true
  try {
    await composeStackApi.deploy(model, pullImages.value)
    message.success(t('buttons.deploy'))
    router.push({ name: 'std_stack_list' })
  } catch (e: any) {
    message.error(e?.message || String(e))
  } finally {
    submitting.value = false
  }
}

// ---- Self-deploy Auto-Deploy branch --------------------------------------
//
// When the stack being edited is the one flagged as the self-deploy source
// stack in Settings, the Deploy button flips to "Auto-Deploy" and routes
// through /api/self-deploy/deploy instead of the usual stack deploy. The
// sidekick orchestrates the restart and the live progress iframe modal
// (shared composable) opens immediately.

const sdConfig = ref<SelfDeployConfig | null>(null)
const autoDeployConfirm = ref(false)

const isSelfDeployStack = computed(() => {
  return !!(sdConfig.value?.enabled && sdConfig.value?.sourceStackId === model.id)
})

const {
  progressOpen,
  progressUrl,
  progressIframe,
  progressIframeFailed,
  progressTimedOut,
  iframeFallbackMessage,
  openProgressFromDeployResult,
  onIframeLoad,
  onIframeError,
} = useAutoDeployProgress()

async function loadSelfDeployConfig() {
  try {
    const r = await selfDeployApi.loadConfig()
    sdConfig.value = r?.data || null
  } catch {
    sdConfig.value = null
  }
}

function autoDeployStack() {
  autoDeployConfirm.value = true
}

async function confirmAutoDeploy() {
  submitting.value = true
  try {
    const r = await selfDeployApi.deploy()
    autoDeployConfirm.value = false
    openProgressFromDeployResult(r?.data)
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    submitting.value = false
  }
}

// ---- Vault secret bindings -------------------------------------------------

const bindings = ref<ComposeStackSecretBinding[]>([])
const bindingsLoading = ref(false)
const vaultSecrets = ref<VaultSecret[]>([])

const vaultSecretOptions = computed(() =>
  vaultSecrets.value.map(s => ({
    label: s.path ? `${s.name} → ${s.path}${s.field ? ' / ' + s.field : ''}` : s.name,
    value: s.id,
  }))
)

const storageOptions = computed(() => [
  { label: t('enums.storage_tmpfs'), value: 'tmpfs' },
  { label: t('enums.storage_volume'), value: 'volume' },
  { label: t('enums.storage_init'), value: 'init' },
])

function vaultSecretName(id: string): string {
  const s = vaultSecrets.value.find(x => x.id === id)
  return s?.name || id
}

function shortHash(h?: string): string {
  return h ? h.slice(0, 8) : ''
}

// driftMap is keyed by binding id so the table can look up state per row
// in O(1) without iterating the drift array on every render.
const driftMap = ref<Record<string, ComposeStackSecretDrift>>({})

function driftBadge(bindingId: string): { label: string; type: string } | null {
  const d = driftMap.value[bindingId]
  if (!d) return null
  switch (d.state) {
    case 'drifted':
      return { label: t('stack_secret.drift_drifted'), type: 'warning' }
    case 'missing':
      return { label: t('stack_secret.drift_missing'), type: 'error' }
    case 'error':
      return { label: t('stack_secret.drift_error'), type: 'error' }
    default:
      // ok and unknown render no badge — the deploy timestamp is sufficient.
      return null
  }
}

async function reloadDrift() {
  if (!model.id) return
  try {
    const r = await composeStackSecretApi.drift(model.id)
    const map: Record<string, ComposeStackSecretDrift> = {}
    for (const d of (r.data as any) || []) map[d.bindingId] = d
    driftMap.value = map
  } catch {
    // Drift check is best-effort; silently ignore so the table still loads.
    driftMap.value = {}
  }
}

const bindingModalOpen = ref(false)
const bindingFormRef = ref()
const savingBinding = ref(false)

// Defaults match the biz validation: tmpfs storage, 0400 mode.
function emptyBinding(): Partial<ComposeStackSecretBinding> {
  return {
    id: '',
    stackId: model.id,
    vaultSecretId: '',
    field: '',
    service: '',
    targetType: 'file',
    targetPath: '',
    envName: '',
    storageMode: 'tmpfs',
    uid: 0,
    gid: 0,
    mode: '0400',
  }
}

const bindingForm = reactive<Partial<ComposeStackSecretBinding>>(emptyBinding())

const bindingRules = {
  vaultSecretId: requiredRule(),
  targetType: requiredRule(),
}

function openBindingModal(b?: ComposeStackSecretBinding) {
  Object.assign(bindingForm, emptyBinding())
  if (b) {
    Object.assign(bindingForm, b)
  }
  bindingModalOpen.value = true
}

async function saveBinding() {
  try {
    await (bindingFormRef.value as any)?.validate()
  } catch {
    return
  }
  // Targeted validation that mirrors the backend so errors surface here
  // instead of as a generic 500 from the save call.
  if (bindingForm.targetType === 'file') {
    if (!bindingForm.targetPath || !bindingForm.targetPath.startsWith('/')) {
      message.error(t('stack_secret.target_path_required'))
      return
    }
  } else if (bindingForm.targetType === 'env') {
    if (!bindingForm.envName) {
      message.error(t('stack_secret.env_name_required'))
      return
    }
  }
  savingBinding.value = true
  try {
    const payload: Partial<ComposeStackSecretBinding> = {
      ...bindingForm,
      stackId: model.id,
    }
    await composeStackSecretApi.save(payload)
    bindingModalOpen.value = false
    message.success(t('texts.action_success'))
    await reloadBindings()
  } catch (e: any) {
    message.error(e?.message || String(e))
  } finally {
    savingBinding.value = false
  }
}

async function deleteBinding(id: string, index: number) {
  try {
    await composeStackSecretApi.delete(id)
    bindings.value.splice(index, 1)
  } catch (e: any) {
    message.error(e?.message || String(e))
  }
}

// ---- Wizard: multi-field binding creation ----------------------------------

const wizardOpen = ref(false)
const wizardStep = ref(1)
const wizardSecretId = ref('')
const wizardFields = ref<string[]>([])
const wizardSelectedFields = ref<string[]>([])
const wizardConfig = ref<{ field: string; service: string; envName: string; targetType: string }[]>([])
const wizardLoadingFields = ref(false)
const wizardSaving = ref(false)

// Extract service names from compose YAML via indent-aware parsing.
// Detects the indent level of the first child under `services:` and
// only captures names at THAT exact level — deeper keys like
// `environment:`, `labels:`, `volumes:` are ignored.
function parseServiceNames(content: string): string[] {
  const lines = (content || '').split('\n')
  let inServices = false
  let serviceIndent = -1
  const names: string[] = []
  for (const line of lines) {
    if (/^services:\s*(#.*)?$/.test(line)) { inServices = true; continue }
    if (!inServices) continue
    // A non-indented line after `services:` means we left the block.
    if (line.length > 0 && /^\S/.test(line)) break
    // Determine the indent of the first service name dynamically
    // (works for both 2-space and 4-space conventions).
    if (serviceIndent < 0) {
      const m = line.match(/^(\s+)\S/)
      if (m) serviceIndent = m[1].length
      else continue
    }
    const leading = line.match(/^(\s*)/)
    if (!leading || leading[1].length !== serviceIndent) continue
    const m = line.match(/^\s+([a-zA-Z0-9_][a-zA-Z0-9_.-]*):\s*(#.*)?$/)
    if (m) names.push(m[1])
  }
  return names
}

const serviceOptions = computed(() => {
  const names = parseServiceNames(model.content)
  return [
    { label: t('stack_secret.all_services'), value: '' },
    ...names.map(n => ({ label: n, value: n })),
  ]
})

function openWizard() {
  wizardStep.value = 1
  wizardSecretId.value = ''
  wizardFields.value = []
  wizardSelectedFields.value = []
  wizardConfig.value = []
  wizardSaving.value = false
  wizardOpen.value = true
}

async function wizardLoadFields(secretId: string) {
  wizardFields.value = []
  wizardSelectedFields.value = []
  if (!secretId) return
  wizardLoadingFields.value = true
  try {
    const r = await vaultSecretApi.preview(secretId)
    const data = r.data as any
    wizardFields.value = data?.fields || []
  } catch (e: any) {
    message.error(e?.message || String(e))
  } finally {
    wizardLoadingFields.value = false
  }
}

const wizardCanNext = computed(() => {
  if (wizardStep.value === 1) return wizardFields.value.length > 0
  if (wizardStep.value === 2) return wizardSelectedFields.value.length > 0
  return true
})

function wizardNext() {
  if (wizardStep.value === 1 && wizardFields.value.length > 0) {
    wizardStep.value = 2
  } else if (wizardStep.value === 2) {
    // Build config rows for each selected field
    wizardConfig.value = wizardSelectedFields.value.map(f => ({
      field: f,
      service: '',
      envName: f, // default: field name as env var
      targetType: 'env',
    }))
    wizardStep.value = 3
  }
}

async function wizardSubmit() {
  wizardSaving.value = true
  try {
    for (const cfg of wizardConfig.value) {
      const binding: Partial<ComposeStackSecretBinding> = {
        stackId: model.id,
        vaultSecretId: wizardSecretId.value,
        field: cfg.field,
        service: cfg.service,
        targetType: cfg.targetType as 'file' | 'env',
        envName: cfg.targetType === 'env' ? cfg.envName : '',
        targetPath: cfg.targetType === 'file' ? `/run/secrets/${cfg.field}` : '',
        storageMode: cfg.targetType === 'file' ? 'tmpfs' : undefined,
      }
      await composeStackSecretApi.save(binding)
    }
    wizardOpen.value = false
    message.success(t('texts.action_success'))
    await reloadBindings()
  } catch (e: any) {
    message.error(e?.message || String(e))
  } finally {
    wizardSaving.value = false
  }
}

async function reloadBindings() {
  if (!model.id) return
  bindingsLoading.value = true
  try {
    const r = await composeStackSecretApi.list(model.id)
    bindings.value = (r.data as any) || []
    // Drift check is independent — we kick it off here so the table stays
    // in sync on every refresh, but don't block the bindings render on it.
    reloadDrift()
  } catch (e: any) {
    message.error(e?.message || String(e))
  } finally {
    bindingsLoading.value = false
  }
}

async function loadVaultSecrets() {
  try {
    const r = await vaultSecretApi.list()
    vaultSecrets.value = (r.data as any) || []
  } catch {
    // Empty list is acceptable; the UI surfaces the no_secrets_hint alert.
    vaultSecrets.value = []
  }
}

onMounted(async () => {
  const r = await hostApi.search('', '', 1, 1000)
  const data = r.data as any
  hosts.value = (data?.items || []).map((h: any) => ({ label: h.name, value: h.id }))

  if (isEdit.value) {
    const s = await composeStackApi.find(route.params.id as string)
    if (s.data) {
      model.id = s.data.id || ''
      model.hostId = s.data.hostId
      model.name = s.data.name
      model.content = s.data.content || ''
      model.envFile = s.data.envFile || ''
      model.errorMessage = s.data.errorMessage || ''
    }
    // Fire in parallel: stack-level artifacts + self-deploy flag check.
    // loadSelfDeployConfig is tolerant to 403/404 (returns null) so
    // users without self_deploy.view still get a clean Deploy button.
    await Promise.all([loadVaultSecrets(), reloadBindings(), loadSelfDeployConfig()])
  } else {
    model.content = '# Paste or author your docker-compose YAML here\n# example:\n# services:\n#   web:\n#     image: nginx:alpine\n#     ports:\n#       - "8080:80"\n'
  }
})
</script>

<style scoped>
.muted {
  color: var(--n-text-color-3, #999);
}
.hash {
  font-size: 11px;
  opacity: 0.75;
}
.ads-iframe-fallback {
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
.ads-iframe-fallback a {
  color: #4b91ff;
  word-break: break-all;
}
</style>
