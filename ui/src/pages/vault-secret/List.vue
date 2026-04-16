<template>
  <x-page-header :subtitle="t('texts.records', { total: filteredModel.length }, filteredModel.length)">
    <template #action>
      <n-space :size="8">
        <n-button secondary size="small" @click="refreshAll" :loading="loading">
          <template #icon>
            <n-icon><refresh-icon /></n-icon>
          </template>
          {{ t('buttons.refresh') }}
        </n-button>
        <n-button type="primary" size="small" @click="$router.push({ name: 'vault_secret_new' })">
          <template #icon>
            <n-icon><add-icon /></n-icon>
          </template>
          {{ t('buttons.new') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>

  <n-space class="page-body" vertical :size="12">
    <n-alert type="info" :show-icon="false">
      {{ t('tips.vault_secret') }}
    </n-alert>

    <!-- Filter bar: free-text + labels multi-select. Filtering is done
         client-side on the full catalog because /list is already cheap. -->
    <n-space :size="12" align="center" wrap>
      <n-input
        size="small"
        v-model:value="filter.text"
        :placeholder="t('vault_secret.filter_placeholder')"
        clearable
        style="min-width: 260px;"
      />
      <n-select
        size="small"
        multiple
        filterable
        clearable
        :placeholder="t('vault_secret.filter_labels')"
        :options="labelOptions"
        v-model:value="filter.labels"
        style="min-width: 260px;"
      />
      <span class="muted" v-if="checked.length">
        {{ t('vault_secret.bulk_selected', { count: checked.length }) }}
      </span>
      <n-popconfirm
        v-if="checked.length"
        :show-icon="false"
        @positive-click="deleteBulk"
      >
        <template #trigger>
          <n-button size="small" type="error" ghost>
            {{ t('vault_secret.bulk_delete') }} ({{ checked.length }})
          </n-button>
        </template>
        {{ t('prompts.delete') }}
      </n-popconfirm>
    </n-space>

    <n-empty
      v-if="!loading && !model.length"
      :description="t('vault_secret.empty_title')"
    >
      <template #extra>
        <n-button type="primary" @click="$router.push({ name: 'vault_secret_new' })">
          {{ t('vault_secret.empty_cta') }}
        </n-button>
      </template>
    </n-empty>

    <n-table
      v-else
      size="small"
      :bordered="true"
      :single-line="false"
    >
      <thead>
        <tr>
          <th style="width: 40px;">
            <n-checkbox
              :checked="allChecked"
              :indeterminate="someChecked"
              @update:checked="toggleAll"
            />
          </th>
          <th>{{ t('fields.name') }}</th>
          <th>{{ t('fields.path') }}</th>
          <th>{{ t('fields.field') }}</th>
          <th>{{ t('vault_secret.versions_col') }}</th>
          <th>{{ t('fields.labels') }}</th>
          <th>{{ t('fields.updated_at') }}</th>
          <th>{{ t('fields.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="s of filteredModel"
          :key="s.id"
          :class="{ 'row-missing': statusState(s) === 'missing' }"
        >
          <td>
            <n-checkbox
              :checked="checked.includes(s.id)"
              @update:checked="(v: boolean) => toggleRow(s.id, v)"
            />
          </td>
          <td>
            <x-anchor :url="{ name: 'vault_secret_detail', params: { id: s.id } }">
              {{ s.name }}
            </x-anchor>
          </td>
          <td><code class="mono">{{ s.path }}</code></td>
          <td>
            <code class="mono" v-if="s.field">{{ s.field }}</code>
            <span v-else class="muted">{{ t('vault_secret.field_all') }}</span>
          </td>
          <td>
            <version-badge
              :state="statusState(s)"
              :current-version="statusMap[s.id]?.currentVersion"
              :total-versions="statusMap[s.id]?.totalVersions"
              :error="statusMap[s.id]?.error"
            />
            <n-spin v-if="loadingStatuses && !statusMap[s.id]" size="small" />
          </td>
          <td>
            <n-space :size="4" inline>
              <n-tag
                v-for="(v, k) of (s.labels || {})"
                :key="k"
                size="small"
                round
              >{{ k }}={{ v }}</n-tag>
              <span v-if="!hasLabels(s)" class="muted">-</span>
            </n-space>
          </td>
          <td>
            <n-time :time="s.updatedAt" format="y-MM-dd HH:mm:ss" />
          </td>
          <td>
            <n-space :size="4" inline>
              <n-button
                size="tiny"
                quaternary
                type="info"
                @click="openPreview(s)"
              >{{ t('buttons.preview') }}</n-button>
              <n-button
                size="tiny"
                quaternary
                type="warning"
                @click="$router.push({ name: 'vault_secret_edit', params: { id: s.id } })"
              >{{ t('buttons.edit') }}</n-button>
              <n-popconfirm :show-icon="false" @positive-click="deleteItem(s.id)">
                <template #trigger>
                  <n-button size="tiny" quaternary type="error">{{ t('buttons.delete') }}</n-button>
                </template>
                {{ t('prompts.delete') }}
              </n-popconfirm>
            </n-space>
          </td>
        </tr>
        <tr v-if="!filteredModel.length && model.length">
          <td colspan="8" style="text-align: center; padding: 24px;">
            <span class="muted">{{ t('vault_secret.filter_empty') }}</span>
          </td>
        </tr>
      </tbody>
    </n-table>
  </n-space>

  <!-- Preview drawer — unchanged from before, shows field names only. -->
  <n-drawer v-model:show="previewOpen" :width="420" placement="right">
    <n-drawer-content :title="t('fields.preview')" closable>
      <n-space vertical :size="12">
        <n-alert type="warning" :show-icon="false">
          {{ t('tips.vault_secret_no_value') }}
        </n-alert>
        <div v-if="previewTarget">
          <div class="preview-item">
            <span class="label">{{ t('fields.name') }}:</span>
            <strong>{{ previewTarget.name }}</strong>
          </div>
          <div class="preview-item">
            <span class="label">{{ t('fields.path') }}:</span>
            <code>{{ previewTarget.path }}</code>
          </div>
          <div class="preview-item" v-if="previewTarget.field">
            <span class="label">{{ t('fields.field') }}:</span>
            <code>{{ previewTarget.field }}</code>
          </div>
        </div>
        <n-spin :show="previewing">
          <n-alert v-if="previewResult && previewResult.exists" type="success">
            <div>{{ t('texts.vault_secret_available_fields') }}:</div>
            <n-space :size="4" style="margin-top: 8px;">
              <n-tag v-for="f of previewResult.fields" :key="f" size="small" type="info">{{ f }}</n-tag>
              <span v-if="!previewResult.fields.length" class="muted">
                {{ t('texts.vault_secret_fields_empty') }}
              </span>
            </n-space>
          </n-alert>
          <n-alert v-else-if="previewResult && !previewResult.exists" type="error">
            {{ t('texts.vault_secret_missing') }}
          </n-alert>
          <n-alert v-else-if="previewError" type="error">
            {{ previewError }}
          </n-alert>
          <div v-else style="min-height: 48px;" />
        </n-spin>
      </n-space>
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import {
  NSpace, NButton, NTable, NPopconfirm, NIcon, NTime, NInput, NAlert, NTag,
  NDrawer, NDrawerContent, NSpin, NSelect, NCheckbox, NEmpty,
} from "naive-ui";
import { AddOutline as AddIcon, RefreshOutline as RefreshIcon } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XAnchor from "@/components/Anchor.vue";
import VersionBadge from "@/components/VersionBadge.vue";
import vaultSecretApi from "@/api/vault-secret";
import type { VaultSecret, VaultSecretPreview, VaultSecretStatus } from "@/api/vault-secret";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const model = ref<VaultSecret[]>([])
const statusMap = ref<Record<string, VaultSecretStatus>>({})
const loading = ref(false)
const loadingStatuses = ref(false)
const filter = reactive<{ text: string; labels: string[] }>({ text: '', labels: [] })
const checked = ref<string[]>([])

// Preview drawer (unchanged)
const previewOpen = ref(false)
const previewing = ref(false)
const previewTarget = ref<VaultSecret | null>(null)
const previewResult = ref<VaultSecretPreview | null>(null)
const previewError = ref('')

function hasLabels(s: VaultSecret): boolean {
  return !!s.labels && Object.keys(s.labels).length > 0
}

// Label set collected from all entries; each option looks like "env=prod".
const labelOptions = computed(() => {
  const set = new Set<string>()
  for (const s of model.value) {
    if (!s.labels) continue
    for (const [k, v] of Object.entries(s.labels)) {
      set.add(`${k}=${v}`)
    }
  }
  return Array.from(set).sort().map(l => ({ label: l, value: l }))
})

const filteredModel = computed(() => {
  const text = filter.text.trim().toLowerCase()
  const labels = filter.labels
  return model.value.filter(s => {
    if (text) {
      const hay = `${s.name} ${s.path}`.toLowerCase()
      if (!hay.includes(text)) return false
    }
    if (labels.length) {
      const entries = Object.entries(s.labels || {}).map(([k, v]) => `${k}=${v}`)
      for (const l of labels) if (!entries.includes(l)) return false
    }
    return true
  })
})

function statusState(s: VaultSecret): 'ok' | 'missing' | 'error' | '' {
  const st = statusMap.value[s.id]
  if (!st) return ''
  if (st.error) return 'error'
  if (!st.exists) return 'missing'
  return 'ok'
}

// Bulk selection
const allChecked = computed(() =>
  filteredModel.value.length > 0 &&
  filteredModel.value.every(s => checked.value.includes(s.id))
)
const someChecked = computed(() =>
  filteredModel.value.some(s => checked.value.includes(s.id)) && !allChecked.value
)
function toggleAll(v: boolean) {
  const ids = filteredModel.value.map(s => s.id)
  if (v) {
    const set = new Set([...checked.value, ...ids])
    checked.value = Array.from(set)
  } else {
    checked.value = checked.value.filter(id => !ids.includes(id))
  }
}
function toggleRow(id: string, v: boolean) {
  if (v) {
    if (!checked.value.includes(id)) checked.value = [...checked.value, id]
  } else {
    checked.value = checked.value.filter(x => x !== id)
  }
}

async function refreshAll() {
  loading.value = true
  try {
    await fetchData()
    // Kick status fetch but don't block the primary render.
    fetchStatuses()
  } finally {
    loading.value = false
  }
}

async function fetchData() {
  const r = await vaultSecretApi.list()
  model.value = r.data || []
  // Drop any selected IDs that no longer exist.
  const ids = new Set(model.value.map(s => s.id))
  checked.value = checked.value.filter(id => ids.has(id))
}

async function fetchStatuses() {
  if (!model.value.length) {
    statusMap.value = {}
    return
  }
  loadingStatuses.value = true
  try {
    const r = await vaultSecretApi.statuses()
    statusMap.value = r.data || {}
  } catch {
    // Best-effort: empty on failure. Badge stays empty.
    statusMap.value = {}
  } finally {
    loadingStatuses.value = false
  }
}

async function deleteItem(id: string) {
  await vaultSecretApi.delete(id)
  model.value = model.value.filter(s => s.id !== id)
  checked.value = checked.value.filter(x => x !== id)
  delete statusMap.value[id]
}

async function deleteBulk() {
  const ids = [...checked.value]
  for (const id of ids) {
    try {
      await vaultSecretApi.delete(id)
    } catch (e: any) {
      window.message?.error?.(e?.message || String(e))
    }
  }
  model.value = model.value.filter(s => !ids.includes(s.id))
  for (const id of ids) delete statusMap.value[id]
  checked.value = []
}

async function openPreview(s: VaultSecret) {
  previewTarget.value = s
  previewResult.value = null
  previewError.value = ''
  previewOpen.value = true
  previewing.value = true
  try {
    const r = await vaultSecretApi.preview(s.id)
    previewResult.value = r.data as VaultSecretPreview
  } catch (e: any) {
    previewError.value = e?.message || String(e)
  } finally {
    previewing.value = false
  }
}

onMounted(refreshAll)
</script>

<style scoped>
.muted {
  color: var(--n-text-color-3, #999);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.preview-item {
  margin-bottom: 6px;
}
.preview-item .label {
  display: inline-block;
  min-width: 72px;
  color: var(--n-text-color-3, #888);
  margin-right: 8px;
}
.row-missing {
  background-color: rgba(208, 48, 80, 0.05);
}
</style>
