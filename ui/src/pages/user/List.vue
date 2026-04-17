<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'user_new' })">
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
    <x-tab>
      <x-tab-pane :href="{ name: 'user_list' }" :active="!$route.query.filter">{{ t('fields.all') }}</x-tab-pane>
      <x-tab-pane
        :href="{ name: 'user_list', query: { filter: tab } }"
        :active="$route.query.filter === tab"
        v-for="tab in ['admins', 'active', 'blocked']"
      >{{ t('fields.' + tab) }}</x-tab-pane>
    </x-tab>
    <n-space :size="12">
      <n-input size="small" v-model:value="args.name" :placeholder="t('fields.name')" clearable />
      <n-input
        size="small"
        v-model:value="args.loginName"
        :placeholder="t('fields.login_name')"
        clearable
      />
      <n-button size="small" type="primary" @click="() => fetchData()">{{ t('buttons.search') }}</n-button>
    </n-space>
    <n-data-table
      remote
      :row-key="row => row.name"
      size="small"
      :columns="columns"
      :data="state.data"
      :pagination="pagination"
      :loading="state.loading"
      @update:page="fetchData"
      @update-page-size="changePageSize"
      scroll-x="max-content"
    />
  </n-space>
</template>

<script setup lang="ts">
import { reactive, watch } from "vue";
import {
  NSpace,
  NInput,
  NButton,
  NIcon,
  NDataTable,
} from "naive-ui";
import {
  AddOutline as AddIcon,
} from "@vicons/ionicons5";
import { useRoute, useRouter } from "vue-router";
import { XTab, XTabPane } from "@/components/tab";
import XPageHeader from "@/components/PageHeader.vue";
import userApi from "@/api/user";
import type { User } from "@/api/user";
import { useDataTable } from "@/utils/data-table";
import { renderButtons, renderLink, renderTag, renderTime } from "@/utils/render";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute();
const router = useRouter();
const args = reactive({
  name: "",
  loginName: "",
});
const columns = [
  {
    title: t('fields.id'),
    key: "id",
    sorter: (a: User, b: User) => (a.id || '').localeCompare(b.id || ''),
    render: (row: User) => renderLink({ name: 'user_detail', params: { id: row.id } }, row.id),
  },
  {
    title: t('fields.name'),
    key: "name",
    sorter: (a: User, b: User) => (a.name || '').localeCompare(b.name || ''),
  },
  {
    title: t('fields.login_name'),
    key: "loginName",
    sorter: (a: User, b: User) => (a.loginName || '').localeCompare(b.loginName || ''),
  },
  {
    title: t('fields.email'),
    key: "email",
    sorter: (a: User, b: User) => (a.email || '').localeCompare(b.email || ''),
  },
  {
    title: t('fields.admin'),
    key: "admin",
    sorter: (a: User, b: User) => (a.admin ? 1 : 0) - (b.admin ? 1 : 0),
    render: (row: User) => t(row.admin ? 'enums.yes' : 'enums.no'),
  },
  {
    title: t('fields.status'),
    key: "status",
    sorter: (a: User, b: User) => (a.status || 0) - (b.status || 0),
    render: (row: User) => renderTag(
      row.status ? t('enums.normal') : t('enums.blocked'),
      row.status ? "success" : "warning"
    ),
  },
  {
    title: t('fields.updated_at'),
    key: "updatedAt",
    sorter: (a: User, b: User) => (a.updatedAt || 0) - (b.updatedAt || 0),
    render: (row: User) => renderTime(row.updatedAt),
  },
  {
    title: t('fields.actions'),
    key: "actions",
    render(row: User, index: number) {
      return renderButtons([
        row.status ?
          { type: 'warning', text: t('buttons.block'), action: () => setStatus(row, 0), prompt: t('prompts.block'), } :
          { type: 'success', text: t('buttons.enable'), action: () => setStatus(row, 1) },
        { type: 'warning', text: t('buttons.edit'), action: () => router.push({ name: 'user_edit', params: { id: row.id } }) },
        { type: 'error', text: t('buttons.delete'), action: () => remove(row, index), prompt: t('prompts.delete') },
      ])
    },
  },
];
const { state, pagination, fetchData, changePageSize } = useDataTable(userApi.search, () => {
  return { ...args, filter: route.query.filter }
})

async function setStatus(u: User, status: number) {
  await userApi.setStatus({ id: u.id, status });
  u.status = status
}

async function remove(u: User, index: number) {
  await userApi.delete(u.id, u.name);
  state.data.splice(index, 1)
}

watch(() => route.query.filter, (newValue: any, oldValue: any) => {
  fetchData()
})
</script>