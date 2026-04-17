<template>
  <x-page-header :subtitle="model.name">
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'vault_secret_list' })">
        <template #icon>
          <n-icon><back-icon /></n-icon>
        </template>
        {{ t('buttons.return') }}
      </n-button>
    </template>
  </x-page-header>

  <n-space class="page-body" vertical :size="16">
    <!-- Catalog entry (persistent pointer) -->
    <x-panel
      :title="t('vault_secret.panel_catalog')"
      :subtitle="t('tips.vault_secret')"
      :collapsed="catalogCollapsed"
      divider="bottom"
    >
      <template #action>
        <n-button secondary size="small" @click="catalogCollapsed = !catalogCollapsed">
          {{ catalogCollapsed ? t('buttons.expand') : t('buttons.collapse') }}
        </n-button>
      </template>

      <n-alert type="warning" :show-icon="false" style="margin-bottom: 12px;">
        {{ t('tips.vault_secret_no_value') }}
      </n-alert>

      <n-form :model="model" :rules="rules" ref="form" label-placement="top">
        <n-grid cols="1 640:2" :x-gap="24">
          <n-form-item-gi :label="t('fields.name')" path="name">
            <n-input
              v-model:value="model.name"
              :placeholder="t('tips.vault_secret_name')"
              :disabled="Boolean(model.id)"
            />
          </n-form-item-gi>
          <n-form-item-gi :label="t('fields.path')" path="path">
            <n-input v-model:value="model.path" :placeholder="t('tips.vault_secret_path')" />
          </n-form-item-gi>
          <n-form-item-gi :label="t('fields.desc')" path="desc">
            <n-input
              v-model:value="model.desc"
              type="textarea"
              :autosize="{ minRows: 1, maxRows: 4 }"
            />
          </n-form-item-gi>
          <n-form-item-gi :label="t('fields.labels')" path="labels" span="2">
            <n-dynamic-input
              v-model:value="labels"
              #="{ value }"
              :on-create="newPair"
            >
              <n-input :placeholder="t('fields.name')" v-model:value="value.name" />
              <div style="height: 34px; line-height: 34px; margin: 0 8px">=</div>
              <n-input :placeholder="t('fields.value')" v-model:value="value.value" />
            </n-dynamic-input>
          </n-form-item-gi>
          <n-gi :span="2">
            <n-button
              type="primary"
              :disabled="submiting"
              :loading="submiting"
              @click.prevent="submit"
            >
              <template #icon>
                <n-icon><save-icon /></n-icon>
              </template>
              {{ t('buttons.save') }}
            </n-button>
          </n-gi>
        </n-grid>
      </n-form>
    </x-panel>

    <!-- Vault status (read-only) — only meaningful for existing records. -->
    <x-panel
      v-if="model.id"
      :title="t('vault_secret.panel_status')"
      divider="bottom"
    >
      <template #action>
        <n-button
          secondary
          size="small"
          :loading="statusLoading"
          @click="refreshStatus"
        >
          <template #icon>
            <n-icon><refresh-icon /></n-icon>
          </template>
          {{ t('vault_secret.status_refresh') }}
        </n-button>
      </template>

      <n-grid cols="1 640:2" :x-gap="24" :y-gap="8">
        <n-gi>
          <div class="status-row">
            <span class="status-label">{{ t('vault_secret.status_full_path') }}:</span>
            <code class="mono">{{ fullPath }}</code>
          </div>
          <div class="status-row">
            <span class="status-label">{{ t('vault_secret.status_version') }}:</span>
            <version-badge
              :state="statusState"
              :current-version="statusInfo?.currentVersion"
              :total-versions="statusInfo?.totalVersions"
              :error="statusInfo?.error"
            />
          </div>
        </n-gi>
        <n-gi>
          <div class="status-row">
            <span class="status-label">{{ t('vault_secret.status_fields') }}:</span>
            <n-space :size="4" inline>
              <n-tag
                v-for="f of previewResult?.fields || []"
                :key="f"
                size="small"
                type="info"
              >{{ f }}</n-tag>
              <span v-if="previewResult && !previewResult.fields.length" class="muted">
                {{ t('texts.vault_secret_fields_empty') }}
              </span>
              <span v-if="!previewResult" class="muted">—</span>
            </n-space>
          </div>
        </n-gi>
      </n-grid>
      <n-alert
        v-if="previewError"
        type="error"
        style="margin-top: 12px;"
      >{{ previewError }}</n-alert>
    </x-panel>

    <!-- Set value (Task 2 — write new KVv2 version). -->
    <x-panel
      v-if="model.id"
      :title="t('vault_secret.panel_write')"
      :subtitle="t('vault_secret.write_hint')"
      divider="bottom"
    >
      <n-alert type="warning" style="margin-bottom: 12px;">
        {{ t('vault_secret.write_warning') }}
      </n-alert>

      <n-form label-placement="top">
        <n-form-item :label="t('vault_secret.write_mode')">
          <n-radio-group v-model:value="writeMode">
            <n-radio value="append">{{ t('vault_secret.write_mode_append') }}</n-radio>
            <n-radio value="replace">{{ t('vault_secret.write_mode_replace') }}</n-radio>
          </n-radio-group>
        </n-form-item>

        <n-form-item :label="t('vault_secret.write_fields')">
          <n-dynamic-input
            v-model:value="writeFields"
            #="{ value }"
            :on-create="newWriteField"
          >
            <n-input
              :placeholder="t('fields.name')"
              v-model:value="value.key"
              style="width: 200px; margin-right: 8px;"
            />
            <n-input
              :placeholder="t('fields.value')"
              v-model:value="value.value"
              type="password"
              show-password-on="click"
              style="flex: 1;"
            />
          </n-dynamic-input>
        </n-form-item>

        <n-button
          type="primary"
          :disabled="writing || !writeFields.length"
          :loading="writing"
          @click="confirmWrite"
        >
          <template #icon>
            <n-icon><save-icon /></n-icon>
          </template>
          {{ t('vault_secret.write_submit') }}
        </n-button>
      </n-form>
    </x-panel>
  </n-space>

  <!-- Confirmation modal for write operation -->
  <n-modal
    v-model:show="writeConfirmOpen"
    preset="dialog"
    :title="t('vault_secret.write_confirm_title')"
    :positive-text="t('buttons.confirm')"
    :negative-text="t('buttons.cancel')"
    :loading="writing"
    @positive-click="doWrite"
  >
    <div>{{ t('vault_secret.write_confirm_body') }}</div>
    <ul style="margin-top: 8px;">
      <li v-for="p of writeFields" :key="p.key">
        <code class="mono">{{ p.key }}</code>
      </li>
    </ul>
    <div class="muted" style="margin-top: 8px;">
      {{ writeMode === 'replace'
          ? t('vault_secret.write_confirm_replace')
          : t('vault_secret.write_confirm_append') }}
    </div>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  NButton, NSpace, NInput, NIcon, NForm, NGrid, NGi, NFormItem, NFormItemGi,
  NDynamicInput, NAlert, NTag, NRadio, NRadioGroup, NModal,
} from "naive-ui";
import {
  ArrowBackCircleOutline as BackIcon,
  SaveOutline as SaveIcon,
  RefreshOutline as RefreshIcon,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XPanel from "@/components/Panel.vue";
import VersionBadge from "@/components/VersionBadge.vue";
import vaultSecretApi from "@/api/vault-secret";
import type { VaultSecret, VaultSecretPreview, VaultSecretStatus } from "@/api/vault-secret";
import settingApi from "@/api/setting";
import { useRoute } from "vue-router";
import { router } from "@/router/router";
import { useForm, requiredRule, customRule } from "@/utils/form";
import { useI18n } from 'vue-i18n'

interface LabelPair { name: string; value: string }
interface WriteField  { key: string; value: string }

const { t } = useI18n()
const route = useRoute()
const form = ref()
const model = ref({} as VaultSecret)
const labels = ref<LabelPair[]>([])
const catalogCollapsed = ref(false)

// Vault status + preview (single refresh call hydrates both)
const statusInfo = ref<VaultSecretStatus | null>(null)
const statusLoading = ref(false)
const previewResult = ref<VaultSecretPreview | null>(null)
const previewError = ref('')

// Write section
const writeMode = ref<'append' | 'replace'>('append')
const writeFields = ref<WriteField[]>([])
const writeConfirmOpen = ref(false)
const writing = ref(false)

// KV prefix (read once to compose the full-path display)
const kvPrefix = ref('')

const nameSegmentRule = customRule(
  (_r, v) => !v || (/^[A-Za-z0-9._-]+$/.test(v) && v.length <= 128),
  t('tips.vault_secret_name_rule'),
)
const rules: any = {
  name: [requiredRule(), nameSegmentRule],
  path: requiredRule(),
}

const fullPath = computed(() => {
  const base = (kvPrefix.value || '').replace(/\/+$/, '')
  const sub = (model.value.path || model.value.name || '').replace(/^\/+/, '')
  return base ? `${base}/${sub}` : sub
})

const statusState = computed<'ok' | 'missing' | 'error' | ''>(() => {
  if (!statusInfo.value) return ''
  if (statusInfo.value.error) return 'error'
  if (!statusInfo.value.exists) return 'missing'
  return 'ok'
})

function newPair(): LabelPair { return { name: '', value: '' } }
function newWriteField(): WriteField { return { key: '', value: '' } }
function pairsToMap(pairs: LabelPair[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const p of pairs) {
    const k = (p.name || '').trim()
    if (k) out[k] = p.value ?? ''
  }
  return out
}
function mapToPairs(m?: Record<string, string>): LabelPair[] {
  if (!m) return []
  return Object.keys(m).map(k => ({ name: k, value: m[k] }))
}

async function saveAction() {
  const payload: Partial<VaultSecret> = {
    ...model.value,
    labels: pairsToMap(labels.value),
  }
  return vaultSecretApi.save(payload)
}

const { submit, submiting } = useForm(form, saveAction, () => {
  window.message.info(t('texts.action_success'))
  router.push({ name: 'vault_secret_list' })
})

async function refreshStatus() {
  if (!model.value.id) return
  statusLoading.value = true
  previewError.value = ''
  try {
    const [sr, pr] = await Promise.all([
      vaultSecretApi.statuses(),
      vaultSecretApi.preview(model.value.id),
    ])
    statusInfo.value = (sr.data && sr.data[model.value.id]) || null
    previewResult.value = pr.data as VaultSecretPreview
  } catch (e: any) {
    previewError.value = e?.message || String(e)
  } finally {
    statusLoading.value = false
  }
}

function confirmWrite() {
  const valid = writeFields.value.filter(f => f.key.trim().length > 0)
  if (!valid.length) {
    window.message?.error?.(t('vault_secret.write_empty'))
    return
  }
  writeConfirmOpen.value = true
}

async function doWrite() {
  const data: Record<string, string> = {}
  for (const f of writeFields.value) {
    const k = f.key.trim()
    if (k) data[k] = f.value
  }
  writing.value = true
  try {
    await vaultSecretApi.write(model.value.id, data, writeMode.value === 'replace')
    window.message?.success?.(t('vault_secret.write_done'))
    writeConfirmOpen.value = false
    writeFields.value = []
    // Refresh status so the new version lands in the badge.
    await refreshStatus()
  } catch (e: any) {
    window.message?.error?.(e?.message || String(e))
  } finally {
    writing.value = false
  }
}

async function fetchData() {
  const id = route.params.id as string
  if (id) {
    const r = await vaultSecretApi.find(id)
    if (r.data) {
      model.value = r.data as VaultSecret
      labels.value = mapToPairs(model.value.labels)
    }
    // Load once the kv_prefix so the full path display is accurate.
    try {
      const s = await settingApi.load()
      kvPrefix.value = (s.data as any)?.vault?.kv_prefix || ''
    } catch { /* non-critical */ }
    // Kick off status refresh on entry.
    refreshStatus()
    catalogCollapsed.value = true
  } else {
    catalogCollapsed.value = false
    model.value = { name: '', path: '', field: '' } as any
  }
}

onMounted(fetchData)
</script>

<style scoped>
.muted {
  color: var(--n-text-color-3, #999);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.status-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}
.status-label {
  color: var(--n-text-color-3, #888);
  min-width: 110px;
}
</style>
