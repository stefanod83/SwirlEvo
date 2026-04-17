<template>
  <x-page-header :subtitle="t('texts.records', { total: model.length }, model.length)" />
  <n-space class="page-body" vertical :size="12">
    <n-data-table
      :row-key="(row: Node) => row.id"
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
} from "naive-ui";
import XAnchor from "@/components/Anchor.vue";
import XPageHeader from "@/components/PageHeader.vue";
import nodeApi from "@/api/node";
import type { Node } from "@/api/node";
import { useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const model = ref([] as Node[]);
const loading = ref(false)

async function deleteNode(id: string) {
  await nodeApi.delete(id);
  model.value = model.value.filter(n => n.id !== id)
}

async function fetchData() {
  loading.value = true
  try {
    let r = await nodeApi.search();
    model.value = r.data || [];
  } finally {
    loading.value = false
  }
}

const columns: any[] = [
  {
    title: t('fields.name'),
    key: 'name',
    sorter: (a: Node, b: Node) => ((a.name || a.hostname) || '').localeCompare((b.name || b.hostname) || ''),
    render: (r: Node) => h(XAnchor, { url: { name: 'node_detail', params: { id: r.id } } }, { default: () => r.name || r.hostname }),
  },
  {
    title: t('fields.role'),
    key: 'role',
    sorter: (a: Node, b: Node) => (a.role || '').localeCompare(b.role || ''),
    render: (r: Node) => h(NTag, {
      round: true, size: 'small',
      type: r.role === 'manager' ? (r.manager?.leader ? 'error' : 'primary') : 'default',
    }, { default: () => r.role }),
  },
  {
    title: t('fields.version'),
    key: 'engineVersion',
    sorter: (a: Node, b: Node) => (a.engineVersion || '').localeCompare(b.engineVersion || ''),
  },
  {
    title: t('fields.cpu'),
    key: 'cpu',
    sorter: (a: Node, b: Node) => (a.cpu || 0) - (b.cpu || 0),
  },
  {
    title: t('fields.memory'),
    key: 'memory',
    sorter: (a: Node, b: Node) => (a.memory || 0) - (b.memory || 0),
    render: (r: Node) => `${r.memory.toFixed(2)} GB`,
  },
  {
    title: t('fields.address'),
    key: 'address',
    sorter: (a: Node, b: Node) => (a.address || '').localeCompare(b.address || ''),
  },
  {
    title: t('fields.state'),
    key: 'state',
    sorter: (a: Node, b: Node) => (a.state || '').localeCompare(b.state || ''),
    render: (r: Node) => h(NTag, {
      round: true, size: 'small',
      type: r.state === 'ready' ? 'success' : 'error',
    }, { default: () => r.state }),
  },
  {
    title: t('fields.actions'),
    key: 'actions',
    render: (r: Node) => h(NSpace, { size: 4, inline: true }, {
      default: () => [
        h(NButton, {
          size: 'tiny', quaternary: true, type: 'warning',
          onClick: () => router.push({ name: 'node_edit', params: { id: r.id } }),
        }, { default: () => t('buttons.edit') }),
        h(NPopconfirm, { showIcon: false, onPositiveClick: () => deleteNode(r.id) }, {
          default: () => t('prompts.delete'),
          trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => t('buttons.delete') }),
        }),
      ],
    }),
  },
]

onMounted(fetchData);
</script>
