<template>
  <x-page-header :subtitle="activeTab === 'list' ? t('texts.records', { total: model.length }, model.length) : undefined">
    <template #action>
      <n-space :size="8" v-if="activeTab === 'list'">
        <n-button secondary size="small" @click="fetchData">
          <template #icon>
            <n-icon><refresh-outline /></n-icon>
          </template>
          {{ t('buttons.refresh') || 'Refresh' }}
        </n-button>
        <n-button secondary size="small" type="error" :disabled="!checkedIds.length" @click="bulkDelete">
          <template #icon>
            <n-icon><trash-outline /></n-icon>
          </template>
          {{ t('buttons.delete') }} ({{ checkedIds.length }})
        </n-button>
        <n-button secondary size="small" :disabled="!selectedHostId" @click="$router.push({ name: 'std_network_new' })">
          <template #icon>
            <n-icon><add-icon /></n-icon>
          </template>
          {{ t('buttons.new') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <x-empty-host-prompt v-if="showEmpty" :resource="t('objects.network', 2)" />
    <n-tabs v-else v-model:value="activeTab" type="line" animated>
      <n-tab-pane name="list" :tab="t('fields.list')">
        <n-data-table
          :row-key="(row: any) => row.id"
          size="small"
          :columns="columns"
          :data="model"
          :loading="loading"
          :checked-row-keys="checkedIds"
          @update:checked-row-keys="(k: any) => checkedIds = k"
          scroll-x="max-content"
        />
      </n-tab-pane>
      <n-tab-pane name="topology" :tab="t('fields.topology')" display-directive="if">
        <NetworkTopology :host-id="selectedHostId" />
      </n-tab-pane>
    </n-tabs>
  </n-space>
</template>

<script setup lang="ts">
import { computed, h, onMounted, ref, watch } from "vue";
import {
  NSpace, NButton, NDataTable, NIcon,
  NTabs, NTabPane,
  useDialog, useMessage,
} from "naive-ui";
import { AddOutline as AddIcon, TrashOutline, RefreshOutline } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XEmptyHostPrompt from "@/components/EmptyHostPrompt.vue";
import NetworkTopology from "./Topology.vue";
import networkApi from "@/api/network";
import type { Network } from "@/api/network";
import { renderButton, renderTag } from "@/utils/render";
import { useStore } from "vuex";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const store = useStore()
const dialog = useDialog()
const message = useMessage()
const selectedHostId = computed(() => store.state.selectedHostId as string | null)
const showEmpty = computed(() => !selectedHostId.value)
const model = ref([] as Network[])
const loading = ref(false)
const checkedIds = ref([] as string[])
const activeTab = ref<'list' | 'topology'>('list')

async function removeOne(id: string, name: string) {
  await networkApi.delete(id, name, selectedHostId.value || '')
  model.value = model.value.filter(n => n.id !== id)
}

async function bulkDelete() {
  if (!checkedIds.value.length) return
  dialog.warning({
    title: t('buttons.delete'),
    content: t('prompts.delete'),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: async () => {
      const errors: string[] = []
      const ids = [...checkedIds.value]
      for (const id of ids) {
        const item = model.value.find(n => n.id === id)
        if (!item) continue
        try { await networkApi.delete(id, item.name, selectedHostId.value || '') }
        catch (e: any) { errors.push(`${item.name}: ${e?.message || e}`) }
      }
      checkedIds.value = []
      if (errors.length) message.error(errors.join('\n'))
      else message.success(t('buttons.delete'))
      fetchData()
    }
  })
}

const columns: any[] = [
  { type: 'selection' },
  {
    title: t('fields.name'),
    key: 'name',
    sorter: (a: Network, b: Network) => (a.name || '').localeCompare(b.name || ''),
    render: (r: Network) => h(NSpace, { size: 6, inline: true, align: 'center' }, {
      default: () => [
        r.name,
        r.unused ? renderTag(t('fields.unused'), 'warning') : null,
      ],
    }),
  },
  {
    title: t('fields.id'),
    key: 'id',
    sorter: (a: Network, b: Network) => (a.id || '').localeCompare(b.id || ''),
  },
  {
    title: t('fields.scope'),
    key: 'scope',
    sorter: (a: Network, b: Network) => (a.scope || '').localeCompare(b.scope || ''),
    render: (r: Network) => renderTag(r.scope, r.scope === 'swarm' ? 'success' : 'default' as any),
  },
  {
    title: t('fields.driver'),
    key: 'driver',
    sorter: (a: Network, b: Network) => (a.driver || '').localeCompare(b.driver || ''),
    render: (r: Network) => renderTag(r.driver, r.driver === 'overlay' ? 'success' : 'default' as any),
  },
  {
    title: t('fields.actions'),
    key: 'actions',
    width: 120,
    render: (r: Network) => renderButton('error', t('buttons.delete'), () => removeOne(r.id, r.name), t('prompts.delete')),
  },
]

async function fetchData() {
  if (!selectedHostId.value) { model.value = []; return }
  loading.value = true
  try {
    const r = await networkApi.search(selectedHostId.value)
    model.value = r.data || []
  } finally {
    loading.value = false
  }
}

watch(selectedHostId, () => {
  model.value = []
  checkedIds.value = []
  fetchData()
})

onMounted(fetchData)
</script>
