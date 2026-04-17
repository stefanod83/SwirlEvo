<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'stack_new' })">
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
    <n-space :size="12">
      <n-input size="small" v-model:value="filter.name" :placeholder="t('fields.name')" clearable />
      <n-button size="small" type="primary" @click="fetchData">{{ t('buttons.search') }}</n-button>
    </n-space>
    <n-data-table
      :row-key="(row: Stack) => row.name"
      size="small"
      :columns="columns"
      :data="model"
      :loading="loading"
      scroll-x="max-content"
    />
  </n-space>
</template>

<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NSpace,
  NButton,
  NIcon,
  NInput,
  NDataTable,
  NPopconfirm,
  NTag,
  NTime,
} from "naive-ui";
import { AddOutline as AddIcon } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XAnchor from "@/components/Anchor.vue";
import stackApi from "@/api/stack";
import type { Stack } from "@/api/stack";
import { useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const model = ref([] as Stack[]);
const loading = ref(false)
const filter = reactive({
  name: "",
  filter: "",
});

async function deleteStack(name: string) {
  await stackApi.delete(name);
  model.value = model.value.filter(s => s.name !== name)
}

async function shutdownStack(s: Stack) {
  await stackApi.shutdown(s.name);
  s.services = []
}

async function deployStack(s: Stack) {
  await stackApi.deploy(s.name);
  s.services = []
}

async function fetchData() {
  loading.value = true
  try {
    let r = await stackApi.search(filter);
    model.value = r.data || [];
  } finally {
    loading.value = false
  }
}

const columns: any[] = [
  {
    title: t('fields.name'),
    key: 'name',
    sorter: (a: Stack, b: Stack) => (a.name || '').localeCompare(b.name || ''),
    render: (r: Stack) => h(XAnchor, { url: { name: 'stack_detail', params: { name: r.name } } }, { default: () => r.name }),
  },
  {
    title: t('objects.service', {}, 2),
    key: 'services',
    sorter: (a: Stack, b: Stack) => (a.services?.length || 0) - (b.services?.length || 0),
    render: (r: Stack) => {
      if (!r.services || !r.services.length) return null
      return h(NSpace, { size: 4 }, {
        default: () => r.services!.map(s => h(NTag, { size: 'small', type: 'primary' }, {
          default: () => h(XAnchor, { url: { name: 'service_detail', params: { name: s } } }, { default: () => s.substring(r.name.length + 1) }),
        })),
      })
    },
  },
  {
    title: t('fields.created_at'),
    key: 'createdAt',
    sorter: (a: Stack, b: Stack) => (a.createdAt || 0) - (b.createdAt || 0),
    render: (r: Stack) => h(NTime, { time: r.createdAt, format: 'y-MM-dd HH:mm:ss' }),
  },
  {
    title: t('fields.updated_at'),
    key: 'updatedAt',
    sorter: (a: Stack, b: Stack) => (a.updatedAt || 0) - (b.updatedAt || 0),
    render: (r: Stack) => h(NTime, { time: r.updatedAt, format: 'y-MM-dd HH:mm:ss' }),
  },
  {
    title: t('fields.actions'),
    key: 'actions',
    render: (r: Stack) => {
      const children: any[] = [
        h(NButton, {
          size: 'tiny', quaternary: true, type: 'warning',
          onClick: () => { router.push({ name: 'stack_edit', params: { name: r.name } }) }
        }, { default: () => t('buttons.edit') }),
        h(NPopconfirm, { showIcon: false, onPositiveClick: () => deployStack(r) }, {
          default: () => t('prompts.deploy'),
          trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'warning' }, { default: () => t('buttons.deploy') }),
        }),
      ]
      if (r.services && r.services.length) {
        children.push(
          h(NPopconfirm, { showIcon: false, onPositiveClick: () => shutdownStack(r) }, {
            default: () => t('prompts.shutdown'),
            trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => t('buttons.shutdown') }),
          })
        )
      }
      children.push(
        h(NPopconfirm, { showIcon: false, onPositiveClick: () => deleteStack(r.name) }, {
          default: () => t('prompts.delete'),
          trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => t('buttons.delete') }),
        })
      )
      return h(NSpace, { size: 4, inline: true }, { default: () => children })
    },
  },
]

onMounted(fetchData);
</script>
