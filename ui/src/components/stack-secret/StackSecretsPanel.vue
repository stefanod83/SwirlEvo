<template>
  <!--
    Vault secret bindings panel. Lifted out of the stack editor so the
    tab-layout can treat it like any other wizard tab. The stack id is
    mandatory: callers render an "save first" notice when they don't
    have one yet (bindings are keyed by stackId).
  -->
  <x-panel
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
          <td><code>{{ vaultSecretName(b.vaultSecretId) }}</code></td>
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

  <!-- Single-binding editor -->
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

  <!-- Multi-field wizard (Step 1/2/3) -->
  <n-modal
    v-model:show="wizardOpen"
    preset="card"
    :title="t('stack_secret.wizard_title')"
    style="width: 720px;"
    :mask-closable="false"
  >
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

    <div v-else-if="wizardStep === 2">
      <div style="margin-bottom: 12px; opacity: 0.7;">{{ t('stack_secret.pick_fields_hint') }}</div>
      <n-checkbox-group v-model:value="wizardSelectedFields">
        <n-space vertical :size="8">
          <n-checkbox v-for="f of wizardFields" :key="f" :value="f" :label="f" />
        </n-space>
      </n-checkbox-group>
    </div>

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
          <tr v-for="(cfg, _i) of wizardConfig" :key="cfg.field">
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
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  NSpace, NButton, NForm, NFormItem, NFormItemGi, NGrid, NInput, NInputNumber,
  NSelect, NCheckbox, NCheckboxGroup, NIcon, NTable, NTag, NPopconfirm, NTime, NTooltip, NSpin,
  NModal, NRadio, NRadioGroup, NAlert,
  useMessage,
} from 'naive-ui'
import {
  AddOutline as AddIcon,
  RefreshOutline as RefreshIcon,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import XPanel from '@/components/Panel.vue'
import composeStackSecretApi from '@/api/compose-stack-secret'
import type {
  ComposeStackSecretBinding,
  ComposeStackSecretDrift,
} from '@/api/compose-stack-secret'
import vaultSecretApi from '@/api/vault-secret'
import type { VaultSecret } from '@/api/vault-secret'
import { requiredRule } from '@/utils/form'

const props = defineProps<{
  stackId: string
  serviceNames: string[]
}>()

const { t } = useI18n()
const message = useMessage()

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
    case 'drifted': return { label: t('stack_secret.drift_drifted'), type: 'warning' }
    case 'missing': return { label: t('stack_secret.drift_missing'), type: 'error' }
    case 'error':   return { label: t('stack_secret.drift_error'),   type: 'error' }
    default: return null
  }
}

async function reloadDrift() {
  if (!props.stackId) return
  try {
    const r = await composeStackSecretApi.drift(props.stackId)
    const map: Record<string, ComposeStackSecretDrift> = {}
    for (const d of (r.data as any) || []) map[d.bindingId] = d
    driftMap.value = map
  } catch {
    driftMap.value = {}
  }
}

const bindingModalOpen = ref(false)
const bindingFormRef = ref()
const savingBinding = ref(false)

function emptyBinding(): Partial<ComposeStackSecretBinding> {
  return {
    id: '',
    stackId: props.stackId,
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
  if (b) Object.assign(bindingForm, b)
  bindingModalOpen.value = true
}

async function saveBinding() {
  try {
    await (bindingFormRef.value as any)?.validate()
  } catch { return }
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
    const payload: Partial<ComposeStackSecretBinding> = { ...bindingForm, stackId: props.stackId }
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

// ---- Wizard: multi-field binding creation ----

const wizardOpen = ref(false)
const wizardStep = ref(1)
const wizardSecretId = ref('')
const wizardFields = ref<string[]>([])
const wizardSelectedFields = ref<string[]>([])
const wizardConfig = ref<{ field: string; service: string; envName: string; targetType: string }[]>([])
const wizardLoadingFields = ref(false)
const wizardSaving = ref(false)

const serviceOptions = computed(() => [
  { label: t('stack_secret.all_services'), value: '' },
  ...props.serviceNames.map((n) => ({ label: n, value: n })),
])

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
    wizardConfig.value = wizardSelectedFields.value.map((f) => ({
      field: f, service: '', envName: f, targetType: 'env',
    }))
    wizardStep.value = 3
  }
}

async function wizardSubmit() {
  wizardSaving.value = true
  try {
    for (const cfg of wizardConfig.value) {
      const binding: Partial<ComposeStackSecretBinding> = {
        stackId: props.stackId,
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
  if (!props.stackId) return
  bindingsLoading.value = true
  try {
    const r = await composeStackSecretApi.list(props.stackId)
    bindings.value = (r.data as any) || []
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
    vaultSecrets.value = []
  }
}

watch(() => props.stackId, async () => {
  if (props.stackId) {
    await Promise.all([loadVaultSecrets(), reloadBindings()])
  }
})

onMounted(async () => {
  if (props.stackId) {
    await Promise.all([loadVaultSecrets(), reloadBindings()])
  }
})
</script>

<style scoped>
.muted { color: var(--n-text-color-3, #999); }
.hash { font-size: 11px; opacity: 0.75; }
</style>
