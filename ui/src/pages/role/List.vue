<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'role_new' })">
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
      <n-input size="small" v-model:value="model.name" :placeholder="t('fields.name')" clearable />
      <n-button size="small" type="primary" @click="fetchData">{{ t('buttons.search') }}</n-button>
    </n-space>
    <n-data-table
      :row-key="(row: Role) => row.id"
      size="small"
      :columns="columns"
      :data="model.roles"
      :loading="loading"
      scroll-x="max-content"
    />
  </n-space>
</template>

<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NSpace,
  NInput,
  NButton,
  NIcon,
  NDataTable,
  NPopconfirm,
  NTime,
} from "naive-ui";
import {
  AddOutline as AddIcon,
} from "@vicons/ionicons5";
import XAnchor from "@/components/Anchor.vue";
import XPageHeader from "@/components/PageHeader.vue";
import roleApi from "@/api/role";
import type { Role } from "@/api/role";
import { useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const loading = ref(false)
const model = reactive({
  name: "",
  roles: [] as Role[],
});

async function deleteRole(r: Role) {
  await roleApi.delete(r.id, r.name);
  model.roles = model.roles.filter(x => x.id !== r.id)
}

async function fetchData() {
  loading.value = true
  try {
    let r = await roleApi.search(model.name);
    model.roles = r.data || [];
  } finally {
    loading.value = false
  }
}

const columns: any[] = [
  {
    title: t('fields.id'),
    key: 'id',
    sorter: (a: Role, b: Role) => (a.id || '').localeCompare(b.id || ''),
    render: (r: Role) => h(XAnchor, { url: { name: 'role_detail', params: { id: r.id } } }, { default: () => r.id }),
  },
  {
    title: t('fields.name'),
    key: 'name',
    sorter: (a: Role, b: Role) => (a.name || '').localeCompare(b.name || ''),
  },
  {
    title: t('fields.desc'),
    key: 'desc',
    sorter: (a: Role, b: Role) => (a.desc || '').localeCompare(b.desc || ''),
  },
  {
    title: t('fields.updated_at'),
    key: 'updatedAt',
    sorter: (a: Role, b: Role) => (a.updatedAt || 0) - (b.updatedAt || 0),
    render: (r: Role) => h(NTime, { time: r.updatedAt, format: 'y-MM-dd HH:mm:ss' }),
  },
  {
    title: t('fields.actions'),
    key: 'actions',
    render: (r: Role) => h(NSpace, { size: 4, inline: true }, {
      default: () => [
        h(NPopconfirm, { showIcon: false, onPositiveClick: () => deleteRole(r) }, {
          default: () => t('prompts.delete'),
          trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => t('buttons.delete') }),
        }),
        h(NButton, {
          size: 'tiny', quaternary: true, type: 'warning',
          onClick: () => router.push({ name: 'role_edit', params: { id: r.id } }),
        }, { default: () => t('buttons.edit') }),
      ],
    }),
  },
]

onMounted(fetchData);
</script>
