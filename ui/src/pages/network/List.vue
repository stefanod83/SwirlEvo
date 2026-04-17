<template>
  <x-page-header :subtitle="t('texts.records', { total: model.length }, model.length)">
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'network_new' })">
        <template #icon>
          <n-icon>
            <add-icon />
          </n-icon>
        </template>
        {{ t('buttons.new') }}
      </n-button>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <x-empty-host-prompt v-if="showEmpty" :resource="t('objects.network', 2)" />
    <n-data-table
      v-else
      :row-key="(row: Network) => row.id"
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
  NTag,
  NIcon,
} from "naive-ui";
import { AddOutline as AddIcon } from "@vicons/ionicons5";
import XAnchor from "@/components/Anchor.vue";
import XPageHeader from "@/components/PageHeader.vue";
import networkApi from "@/api/network";
import type { Network } from "@/api/network";
import { computed, watch } from "vue";
import { useStore } from "vuex";
import XEmptyHostPrompt from "@/components/EmptyHostPrompt.vue";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const store = useStore()
const isStandalone = computed(() => store.state.mode === 'standalone')
const selectedHostId = computed(() => store.state.selectedHostId as string | null)
const showEmpty = computed(() => isStandalone.value && !selectedHostId.value)
const model = ref([] as Network[]);
const loading = ref(false)

async function deleteNetwork(id: string, name: string) {
  await networkApi.delete(id, name);
  model.value = model.value.filter(n => n.id !== id)
}

async function fetchData() {
  loading.value = true
  try {
    let r = await networkApi.search();
    model.value = r.data || [];
  } finally {
    loading.value = false
  }
}

const columns: any[] = [
  {
    title: t('fields.name'),
    key: 'name',
    sorter: (a: Network, b: Network) => (a.name || '').localeCompare(b.name || ''),
    render: (r: Network) => h(NSpace, { size: 6, align: 'center', inline: true }, {
      default: () => [
        h(XAnchor, { url: { name: 'network_detail', params: { name: r.name } } }, { default: () => r.name }),
        r.unused ? h(NTag, { round: true, size: 'small', type: 'warning' }, { default: () => t('fields.unused') }) : null,
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
    render: (r: Network) => h(NTag, { round: true, size: 'small', type: r.scope === 'swarm' ? 'success' : 'default' }, { default: () => r.scope }),
  },
  {
    title: t('fields.driver'),
    key: 'driver',
    sorter: (a: Network, b: Network) => (a.driver || '').localeCompare(b.driver || ''),
    render: (r: Network) => h(NTag, { round: true, size: 'small', type: r.driver === 'overlay' ? 'success' : 'default' }, { default: () => r.driver }),
  },
  {
    title: t('fields.actions'),
    key: 'actions',
    render: (r: Network) => h(NPopconfirm, { showIcon: false, onPositiveClick: () => deleteNetwork(r.id, r.name) }, {
      default: () => t('prompts.delete'),
      trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => t('buttons.delete') }),
    }),
  },
]

watch(selectedHostId, (v) => { if (v || !isStandalone.value) fetchData() })
onMounted(() => { if (!showEmpty.value) fetchData() })
</script>
