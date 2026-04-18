<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'secret_new' })">
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
      <n-button size="small" type="primary" @click="() => fetchData()">{{ t('buttons.search') }}</n-button>
    </n-space>
    <n-data-table
      remote
      :row-key="(c: Secret) => c.id"
      size="small"
      :columns="columns"
      :data="paginatedData"
      :pagination="pagination"
      :loading="state.loading"
      @update:page="changePage"
      @update-page-size="changePageSize"
      @update:sorter="handleSorterChange"
      scroll-x="max-content"
    />
  </n-space>
</template>

<script setup lang="ts">
import { reactive, ref } from "vue";
import {
  NSpace,
  NButton,
  NIcon,
  NInput,
  NDataTable,
} from "naive-ui";
import { AddOutline as AddIcon } from "@vicons/ionicons5";
import { useRouter } from "vue-router";
import XPageHeader from "@/components/PageHeader.vue";
import secretApi from "@/api/secret";
import type { Secret } from "@/api/secret";
import { renderButtons, renderLink } from "@/utils/render";
import { useDataTable } from "@/utils/data-table";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter();
const model = ref([] as Secret[]);
const filter = reactive({
  name: "",
});
const columns = [
  {
    title: t('fields.id'),
    key: "id",
    fixed: "left" as const,
    sorter: (a: Secret, b: Secret) => (a.id || '').localeCompare(b.id || ''),
    render: (c: Secret) => renderLink({ name: 'secret_detail', params: { id: c.id } }, c.id),
  },
  {
    title: t('fields.name'),
    key: "name",
    sorter: (a: Secret, b: Secret) => (a.name || '').localeCompare(b.name || ''),
  },
  {
    title: t('fields.created_at'),
    key: "createdAt",
    sorter: (a: Secret, b: Secret) => (a.createdAt || '').localeCompare(b.createdAt || ''),
  },
  {
    title: t('fields.updated_at'),
    key: "updatedAt",
    sorter: (a: Secret, b: Secret) => (a.updatedAt || '').localeCompare(b.updatedAt || ''),
  },
  {
    title: t('fields.actions'),
    key: "actions",
    render(c: Secret, index: number) {
      return renderButtons([
        {
          type: 'error',
          text: t('buttons.delete'),
          action: () => deleteSecret(c.id, index),
          prompt: t('prompts.delete'),
        },
        {
          type: 'warning',
          text: t('buttons.edit'),
          action: () => router.push({ name: 'secret_edit', params: { id: c.id } }),
        },
      ])
    },
  },
];
const { state, pagination, fetchData, changePage, changePageSize, paginatedData, handleSorterChange, setSortColumns } = useDataTable(secretApi.search, filter, { remote: false })
setSortColumns(columns)

async function deleteSecret(id: string, _index: number) {
  await secretApi.delete(id);
  const i = (state.data as Secret[]).findIndex(c => c.id === id)
  if (i >= 0) state.data.splice(i, 1)
}
</script>