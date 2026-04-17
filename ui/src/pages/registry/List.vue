<template>
  <x-page-header :subtitle="t('texts.records', { total: model.length }, model.length)">
    <template #action>
      <n-space :size="8">
        <n-button secondary size="small" @click="refreshAll">
          <template #icon>
            <n-icon><refresh-icon /></n-icon>
          </template>
          {{ t('buttons.refresh') }}
        </n-button>
        <n-button secondary size="small" @click="$router.push({ name: 'registry_new' })">
          <template #icon>
            <n-icon><add-icon /></n-icon>
          </template>
          {{ t('buttons.new') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <n-data-table
      :row-key="(row: Registry) => row.id"
      size="small"
      :columns="columns"
      :data="model"
      :loading="loading"
      scroll-x="max-content"
    />
  </n-space>
</template>

<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import {
  NSpace, NButton, NDataTable, NPopconfirm, NIcon, NTime, NTag, NTooltip, NSpin,
} from "naive-ui";
import { AddOutline as AddIcon, RefreshOutline as RefreshIcon } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XAnchor from "@/components/Anchor.vue";
import registryApi from "@/api/registry";
import type { Registry } from "@/api/registry";
import { useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const loading = ref(false)
const model = ref<Registry[]>([])
const pingMap = ref<Record<string, { ok: boolean; error?: string }>>({})

async function deleteRegistry(id: string) {
  await registryApi.delete(id);
  model.value = model.value.filter(r => r.id !== id)
  delete pingMap.value[id]
}

async function fetchData() {
  loading.value = true
  try {
    const r = await registryApi.search();
    model.value = r.data || [];
  } finally {
    loading.value = false
  }
}

async function pingAll() {
  pingMap.value = {}
  for (const r of model.value) {
    // Fire pings in parallel
    registryApi.ping(r.id).then(res => {
      pingMap.value = { ...pingMap.value, [r.id]: res.data || { ok: false, error: 'unknown' } }
    }).catch(e => {
      pingMap.value = { ...pingMap.value, [r.id]: { ok: false, error: e?.message || String(e) } }
    })
  }
}

async function refreshAll() {
  await fetchData()
  pingAll()
}

const columns: any[] = [
  {
    title: t('fields.name'),
    key: 'name',
    sorter: (a: Registry, b: Registry) => (a.name || '').localeCompare(b.name || ''),
    render: (r: Registry) => h(XAnchor, { url: { name: 'registry_detail', params: { id: r.id } } }, { default: () => r.name }),
  },
  {
    title: t('fields.address'),
    key: 'url',
    sorter: (a: Registry, b: Registry) => (a.url || '').localeCompare(b.url || ''),
  },
  {
    title: t('fields.status'),
    key: 'status',
    sorter: (a: Registry, b: Registry) => {
      const av = pingMap.value[a.id]?.ok ? 1 : 0
      const bv = pingMap.value[b.id]?.ok ? 1 : 0
      return av - bv
    },
    render: (r: Registry) => {
      const ping = pingMap.value[r.id]
      if (!ping) return h(NSpin, { size: 'small' })
      return h(NTooltip, { trigger: 'hover' }, {
        trigger: () => h(NTag, { size: 'small', type: ping.ok ? 'success' : 'error' }, {
          default: () => ping.ok ? t('registry.status_ok') : t('registry.status_error'),
        }),
        default: () => ping.ok ? t('registry.status_reachable') : ping.error,
      })
    },
  },
  {
    title: t('fields.login_name'),
    key: 'username',
    sorter: (a: Registry, b: Registry) => (a.username || '').localeCompare(b.username || ''),
  },
  {
    title: t('fields.updated_at'),
    key: 'updatedAt',
    sorter: (a: Registry, b: Registry) => (a.updatedAt || 0) - (b.updatedAt || 0),
    render: (r: Registry) => h(NTime, { time: r.updatedAt, format: 'y-MM-dd HH:mm:ss' }),
  },
  {
    title: t('fields.actions'),
    key: 'actions',
    render: (r: Registry) => h(NSpace, { size: 4, inline: true }, {
      default: () => [
        h(NButton, {
          size: 'tiny', quaternary: true, type: 'warning',
          onClick: () => router.push({ name: 'registry_edit', params: { id: r.id } }),
        }, { default: () => t('buttons.edit') }),
        h(NPopconfirm, { showIcon: false, onPositiveClick: () => deleteRegistry(r.id) }, {
          default: () => t('prompts.delete'),
          trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => t('buttons.delete') }),
        }),
      ],
    }),
  },
]

onMounted(refreshAll)
</script>
