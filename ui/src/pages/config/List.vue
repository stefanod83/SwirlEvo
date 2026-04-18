<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'config_new' })">
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
      :row-key="(c: Config) => c.id"
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
import configApi from "@/api/config";
import type { Config } from "@/api/config";
import { renderButtons, renderLink } from "@/utils/render";
import { useDataTable } from "@/utils/data-table";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter();
const filter = reactive({
  name: "",
});
const columns = [
  {
    title: t('fields.id'),
    key: "id",
    fixed: "left" as const,
    sorter: (a: Config, b: Config) => (a.id || '').localeCompare(b.id || ''),
    render: (c: Config) => renderLink({ name: 'config_detail', params: { id: c.id } }, c.id),
  },
  {
    title: t('fields.name'),
    key: "name",
    sorter: (a: Config, b: Config) => (a.name || '').localeCompare(b.name || ''),
  },
  {
    title: t('fields.created_at'),
    key: "createdAt",
    sorter: (a: Config, b: Config) => (a.createdAt || '').localeCompare(b.createdAt || ''),
  },
  {
    title: t('fields.updated_at'),
    key: "updatedAt",
    sorter: (a: Config, b: Config) => (a.updatedAt || '').localeCompare(b.updatedAt || ''),
  },
  {
    title: t('fields.actions'),
    key: "actions",
    render(c: Config, index: number) {
      return renderButtons([
        {
          type: 'error',
          text: t('buttons.delete'),
          action: () => deleteConfig(c.id, index),
          prompt: t('prompts.delete'),
        },
        {
          type: 'warning',
          text: t('buttons.edit'),
          action: () => router.push({ name: 'config_edit', params: { id: c.id } }),
        },
      ])
    },
  },
];
const { state, pagination, fetchData, changePage, changePageSize, paginatedData, handleSorterChange, setSortColumns } = useDataTable(configApi.search, filter, { remote: false })
setSortColumns(columns)

async function deleteConfig(id: string, _index: number) {
  await configApi.delete(id);
  const i = (state.data as Config[]).findIndex(c => c.id === id)
  if (i >= 0) state.data.splice(i, 1)
}
</script>