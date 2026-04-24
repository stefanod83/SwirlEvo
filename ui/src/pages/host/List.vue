<template>
  <x-page-header :subtitle="t('texts.records', { total: total }, total)">
    <template #action>
      <n-space :size="8">
        <n-button secondary size="small" @click="fetchData">
          <template #icon>
            <n-icon><refresh-outline /></n-icon>
          </template>
          {{ t('buttons.refresh') || 'Refresh' }}
        </n-button>
        <n-button secondary size="small" @click="$router.push({ name: 'host_new' })">
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
      :row-key="(row: Host) => row.id"
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
  NSpace,
  NButton,
  NDataTable,
  NPopconfirm,
  NIcon,
  NTime,
  NTag,
} from "naive-ui";
import { AddOutline as AddIcon, RefreshOutline } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XAnchor from "@/components/Anchor.vue";
import * as hostApi from "@/api/host";
import type { Host } from "@/api/host";
import { renderAddonBadges } from "@/utils/addon-badges";
import { useStore } from "vuex";
import { useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const store = useStore();
const router = useRouter();
const model = ref([] as Host[]);
const total = ref(0);
const loading = ref(false);

function statusType(status: string): 'default' | 'error' | 'info' | 'success' | 'warning' {
  switch (status) {
    case 'connected': return 'success'
    case 'error': return 'error'
    default: return 'warning'
  }
}

// enabledAddonsOf decodes Host.AddonConfigExtract (JSON string) and
// returns the list of addon keys flagged `enabled=true` on the host.
// Badge order mirrors the column-picker order used in the stack list
// (traefik, sablier, watchtower, backup, registry-cache). Safe on
// malformed / empty input.
function enabledAddonsOf(h: Host): string[] {
  const raw = h.addonConfigExtract
  if (!raw) return []
  let blob: any
  try { blob = JSON.parse(raw) } catch { return [] }
  const out: string[] = []
  if (blob?.traefik?.enabled) out.push('traefik')
  if (blob?.sablier?.enabled) out.push('sablier')
  if (blob?.watchtower?.enabled) out.push('watchtower')
  if (blob?.backup?.enabled) out.push('backup')
  if (blob?.registryCache?.enabled) out.push('registry-cache')
  return out
}

async function deleteHost(id: string, name: string) {
  await hostApi.remove(id, name);
  model.value = model.value.filter(h => h.id !== id)
  total.value--
  await store.dispatch('reloadHosts')
}

async function syncHost(id: string) {
  await hostApi.sync(id);
  await fetchData();
}

async function fetchData() {
  loading.value = true
  try {
    let r = await hostApi.search();
    const data = r.data as any;
    model.value = data?.items || [];
    total.value = data?.total || 0;
  } finally {
    loading.value = false
  }
}

const columns: any[] = [
  {
    // Narrow colour column — left-most so the operator scans
    // top-to-bottom and immediately maps colours to host names.
    title: '',
    key: 'color',
    width: 8,
    render: (r: Host) => h('div', {
      style: r.color
        ? `width:4px;height:22px;border-radius:2px;background:${r.color}`
        : 'width:4px;height:22px',
      title: r.color || undefined,
    }),
  },
  {
    title: t('fields.name'),
    key: 'name',
    sorter: (a: Host, b: Host) => (a.name || '').localeCompare(b.name || ''),
    render: (r: Host) => h(XAnchor, { url: { name: 'host_detail', params: { id: r.id } } }, { default: () => r.name }),
  },
  {
    title: 'Endpoint',
    key: 'endpoint',
    sorter: (a: Host, b: Host) => (a.endpoint || '').localeCompare(b.endpoint || ''),
    render: (r: Host) => h('code', null, r.endpoint),
  },
  {
    title: t('fields.status'),
    key: 'status',
    sorter: (a: Host, b: Host) => (a.status || '').localeCompare(b.status || ''),
    render: (r: Host) => h(NTag, { type: statusType(r.status), size: 'small' }, { default: () => r.status }),
  },
  {
    title: 'Engine',
    key: 'engineVersion',
    sorter: (a: Host, b: Host) => (a.engineVersion || '').localeCompare(b.engineVersion || ''),
    render: (r: Host) => r.engineVersion || '-',
  },
  {
    title: 'OS/Arch',
    key: 'os',
    render: (r: Host) => r.os && r.arch ? r.os + '/' + r.arch : '-',
  },
  {
    title: t('fields.tags') || 'Addons',
    key: 'addons',
    render: (r: Host) => renderAddonBadges(enabledAddonsOf(r)) || '-',
  },
  {
    title: t('fields.updated_at'),
    key: 'updatedAt',
    sorter: (a: Host, b: Host) => (a.updatedAt || 0) - (b.updatedAt || 0),
    render: (r: Host) => h(NTime, { time: r.updatedAt, format: 'y-MM-dd HH:mm:ss' }),
  },
  {
    title: t('fields.actions'),
    key: 'actions',
    render: (r: Host) => h(NSpace, { size: 4, inline: true }, {
      default: () => [
        h(NButton, {
          size: 'tiny', quaternary: true, type: 'info',
          onClick: () => syncHost(r.id),
        }, { default: () => 'Sync' }),
        h(NButton, {
          size: 'tiny', quaternary: true, type: 'warning',
          onClick: () => router.push({ name: 'host_edit', params: { id: r.id } }),
        }, { default: () => t('buttons.edit') }),
        h(NPopconfirm, { showIcon: false, onPositiveClick: () => deleteHost(r.id, r.name) }, {
          default: () => t('prompts.delete'),
          trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => t('buttons.delete') }),
        }),
      ],
    }),
  },
]

onMounted(fetchData);
</script>
