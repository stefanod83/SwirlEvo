<template>
  <x-page-header :subtitle="t('texts.records', { total: total }, total)">
    <template #action>
      <n-space :size="8">
        <n-button secondary size="small" @click="fetchData">
          <template #icon>
            <n-icon><refresh-icon /></n-icon>
          </template>
          {{ t('buttons.refresh') }}
        </n-button>
        <n-button secondary size="small" @click="$router.push({ name: 'vault_secret_new' })">
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
    <n-space :size="12">
      <n-input
        size="small"
        v-model:value="filter.name"
        :placeholder="t('fields.name')"
        clearable
        @keyup.enter="onSearch"
      />
      <n-button size="small" type="primary" @click="onSearch">{{ t('buttons.search') }}</n-button>
    </n-space>
    <n-table size="small" :bordered="true" :single-line="false">
      <thead>
        <tr>
          <th>{{ t('fields.name') }}</th>
          <th>{{ t('fields.path') }}</th>
          <th>{{ t('fields.field') }}</th>
          <th>{{ t('fields.labels') }}</th>
          <th>{{ t('fields.updated_at') }}</th>
          <th>{{ t('fields.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(s, index) of model" :key="s.id">
          <td>
            <x-anchor :url="{ name: 'vault_secret_detail', params: { id: s.id } }">{{ s.name }}</x-anchor>
          </td>
          <td><code>{{ s.path }}</code></td>
          <td><code>{{ s.field || '-' }}</code></td>
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
            <n-popconfirm :show-icon="false" @positive-click="deleteItem(s.id, index)">
              <template #trigger>
                <n-button size="tiny" quaternary type="error">{{ t('buttons.delete') }}</n-button>
              </template>
              {{ t('prompts.delete') }}
            </n-popconfirm>
          </td>
        </tr>
        <tr v-if="!model.length">
          <td colspan="6" style="text-align: center; padding: 24px;">
            <span class="muted">{{ t('texts.records', { total: 0 }, 0) }}</span>
          </td>
        </tr>
      </tbody>
    </n-table>
    <n-pagination
      v-model:page="pageIndex"
      v-model:page-size="pageSize"
      :item-count="total"
      :page-sizes="[10, 20, 50, 100]"
      show-size-picker
      @update:page="fetchData"
      @update:page-size="fetchData"
    />
  </n-space>

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
import { onMounted, reactive, ref } from "vue";
import {
  NSpace,
  NButton,
  NTable,
  NPopconfirm,
  NIcon,
  NTime,
  NInput,
  NPagination,
  NAlert,
  NTag,
  NDrawer,
  NDrawerContent,
  NSpin,
} from "naive-ui";
import { AddOutline as AddIcon, RefreshOutline as RefreshIcon } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XAnchor from "@/components/Anchor.vue";
import vaultSecretApi from "@/api/vault-secret";
import type { VaultSecret, VaultSecretPreview } from "@/api/vault-secret";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const model = ref([] as VaultSecret[])
const total = ref(0)
const pageIndex = ref(1)
const pageSize = ref(20)
const filter = reactive({ name: "" })

const previewOpen = ref(false)
const previewing = ref(false)
const previewTarget = ref<VaultSecret | null>(null)
const previewResult = ref<VaultSecretPreview | null>(null)
const previewError = ref("")

function hasLabels(s: VaultSecret): boolean {
  return !!s.labels && Object.keys(s.labels).length > 0
}

async function onSearch() {
  pageIndex.value = 1
  await fetchData()
}

async function deleteItem(id: string, index: number) {
  await vaultSecretApi.delete(id)
  model.value.splice(index, 1)
  total.value--
}

async function fetchData() {
  const r = await vaultSecretApi.search({
    name: filter.name,
    pageIndex: pageIndex.value,
    pageSize: pageSize.value,
  })
  model.value = r.data?.items || []
  total.value = r.data?.total || 0
}

async function openPreview(s: VaultSecret) {
  previewTarget.value = s
  previewResult.value = null
  previewError.value = ""
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

onMounted(fetchData)
</script>

<style scoped>
.muted {
  color: var(--n-text-color-3, #999);
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
</style>
