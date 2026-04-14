<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'std_stack_new' })">
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
      <n-select
        filterable
        clearable
        size="small"
        :consistent-menu-width="false"
        :placeholder="t('objects.host')"
        v-model:value="filter.hostId"
        :options="hosts"
        style="width: 240px"
        v-if="hosts && hosts.length"
      />
      <n-input size="small" v-model:value="filter.name" :placeholder="t('fields.name')" clearable />
      <n-button size="small" type="primary" @click="() => fetchData()">{{ t('buttons.search') }}</n-button>
    </n-space>
    <n-data-table
      remote
      :row-key="(row: any) => row.hostId + '|' + row.name"
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
import { h, onMounted, reactive, ref } from "vue";
import {
  NSpace, NButton, NButtonGroup, NDataTable, NInput, NSelect, NIcon, NTooltip,
  useDialog, useMessage,
} from "naive-ui";
import {
  AddOutline as AddIcon,
  PlayOutline, StopOutline, TrashOutline, CreateOutline, RefreshOutline,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import composeStackApi from "@/api/compose_stack";
import type { ComposeStackSummary } from "@/api/compose_stack";
import * as hostApi from "@/api/host";
import { useDataTable } from "@/utils/data-table";
import { renderLink, renderTag } from "@/utils/render";
import { useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const dialog = useDialog()
const message = useMessage()
const filter = reactive({ hostId: '', name: '' })
const hosts: any = ref([])

function actionButton(type: 'default' | 'error' | 'warning' | 'success' | 'info', iconCmp: any, tooltip: string, disabled: boolean, onClick: () => void) {
  return h(NTooltip, { trigger: 'hover' }, {
    trigger: () => h(NButton, { size: 'tiny', quaternary: true, type, disabled, onClick }, { icon: () => h(NIcon, null, { default: () => h(iconCmp) }) }),
    default: () => tooltip,
  })
}

async function runAction(fn: () => Promise<any>, msg: string) {
  try { await fn(); message.success(msg); await fetchData() }
  catch (e: any) { message.error(e?.message || String(e)) }
}

function confirmRemove(s: ComposeStackSummary) {
  dialog.warning({
    title: t('buttons.delete'),
    content: t('prompts.delete'),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: () => runAction(() => composeStackApi.remove(s.id, false), t('buttons.delete')),
  })
}

const columns = [
  {
    title: t('objects.host'),
    key: "hostName",
    render: (s: ComposeStackSummary) => s.hostName || s.hostId || '',
  },
  {
    title: t('fields.name'),
    key: "name",
    render: (s: ComposeStackSummary) => {
      if (!s.id) return s.name + ' (external)'
      return renderLink({ name: 'std_stack_detail', params: { id: s.id } }, s.name)
    },
  },
  {
    title: t('fields.status'),
    key: "status",
    render: (s: ComposeStackSummary) => {
      const type = s.status === 'active' ? 'success' : (s.status === 'partial' ? 'warning' : 'default')
      return renderTag(s.status || '-', type as any)
    }
  },
  {
    title: t('fields.services') || 'Services',
    key: "services",
  },
  {
    title: t('fields.running') || 'Running',
    key: "running",
    render: (s: ComposeStackSummary) => `${s.running}/${s.containers}`,
  },
  {
    title: t('fields.actions'),
    key: "actions",
    width: 260,
    render(s: ComposeStackSummary) {
      if (!s.id) return h('span', { style: 'color:#999' }, t('texts.external_stack') || 'external — import to manage')
      const buttons = [
        actionButton('success', PlayOutline, t('buttons.start'), s.status === 'active',
          () => runAction(() => composeStackApi.start(s.id), t('buttons.start'))),
        actionButton('warning', StopOutline, t('buttons.stop'), s.status === 'inactive',
          () => runAction(() => composeStackApi.stop(s.id), t('buttons.stop'))),
        actionButton('info', RefreshOutline, t('buttons.deploy'), false,
          () => router.push({ name: 'std_stack_edit', params: { id: s.id } })),
        actionButton('default', CreateOutline, t('buttons.edit'), false,
          () => router.push({ name: 'std_stack_edit', params: { id: s.id } })),
        actionButton('error', TrashOutline, t('buttons.delete'), false, () => confirmRemove(s)),
      ]
      return h(NButtonGroup, null, { default: () => buttons })
    },
  },
];
const { state, pagination, fetchData, changePageSize } = useDataTable(composeStackApi.search, filter, false)

onMounted(async () => {
  const r = await hostApi.search('', '', 1, 1000)
  const data = r.data as any
  hosts.value = (data?.items || []).map((h: any) => ({ label: h.name, value: h.id }))
  fetchData()
})
</script>
