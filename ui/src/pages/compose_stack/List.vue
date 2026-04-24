<template>
  <x-page-header :subtitle="hostSubtitle">
    <template #action>
      <n-space :size="8">
        <n-button secondary size="small" @click="() => fetchData()">
          <template #icon>
            <n-icon><refresh-outline /></n-icon>
          </template>
          {{ t('buttons.refresh') || 'Refresh' }}
        </n-button>
        <n-button secondary size="small" @click="$router.push({ name: 'std_stack_new' })">
          <template #icon>
            <n-icon><add-icon /></n-icon>
          </template>
          {{ t('buttons.new') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <x-empty-host-prompt v-if="showEmpty" :resource="t('objects.stack', 2)" />
    <template v-else>
      <n-space :size="12">
        <n-input size="small" v-model:value="filter.name" :placeholder="t('fields.name')" clearable />
        <n-button size="small" type="primary" @click="() => fetchData()">{{ t('buttons.search') }}</n-button>
      </n-space>
      <n-data-table
        remote
        :row-key="(row: any) => row.hostId + '|' + row.name"
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
    </template>
  </n-space>
</template>

<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NSpace, NButton, NButtonGroup, NDataTable, NInput, NIcon, NTag, NTooltip,
  useDialog, useMessage,
} from "naive-ui";
import { computed, watch } from "vue";
import { useStore } from "vuex";
import XEmptyHostPrompt from "@/components/EmptyHostPrompt.vue";
import {
  AddOutline as AddIcon,
  PlayOutline, StopOutline, TrashOutline, CreateOutline, RefreshOutline, EyeOutline, DownloadOutline,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import composeStackApi from "@/api/compose_stack";
import composeStackSecretApi from "@/api/compose-stack-secret";
import type { ComposeStackSummary } from "@/api/compose_stack";
import { useDataTable } from "@/utils/data-table";
import { renderLink, renderTag } from "@/utils/render";
import { renderAddonBadges } from "@/utils/addon-badges";
import { buildZip } from "@/utils/zip";
import { useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const dialog = useDialog()
const message = useMessage()
const store = useStore()
const selectedHostId = computed(() => store.state.selectedHostId as string | null)
const filter = reactive({ hostId: '', name: '' })
const showEmpty = computed(() => !selectedHostId.value)
const hostSubtitle = computed(() => {
  const h = (store.state.hosts as any[]).find(x => x.id === selectedHostId.value)
  return h?.name || ''
})

// Colour + short label for each addon tag. Keeping the palette
// unobtrusive (tiny round tag, quaternary-ish intensities) matches
// the "a colpo d'occhio, poco invadente" brief — the operator sees
// at a glance which addons are active on each stack without the
// badges stealing focus from the stack name.

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

// removeWithForceConfirm encapsulates the two-step delete:
//   1. call remove() without force — backend either deletes or returns
//      { volumesContainData: true, volumes: [...] };
//   2. if backend responded with that flag, open a second dialog that
//      lists the volume names and calls remove() with force=true.
async function removeWithForceConfirm(base: any, removeVolumes: boolean) {
  try {
    const r = await composeStackApi.remove({ ...base, removeVolumes, force: false })
    if (r.data?.volumesContainData && (r.data?.volumes?.length || 0) > 0) {
      dialog.error({
        title: t('messages.volumes_contain_data_confirm'),
        content: (r.data?.volumes || []).join('\n'),
        positiveText: t('buttons.force_delete') || 'Force delete',
        negativeText: t('buttons.cancel'),
        onPositiveClick: () => runAction(
          () => composeStackApi.remove({ ...base, removeVolumes, force: true }),
          t('buttons.delete'),
        ),
      })
      return
    }
    message.success(t('buttons.delete'))
    await fetchData()
  } catch (e: any) {
    message.error(e?.message || String(e))
  }
}

async function downloadStack(id: string, name: string) {
  try {
    const r = await composeStackApi.find(id)
    const content = r.data?.content || ''
    if (!content) { message.warning('No compose content available'); return }
    const envContent = r.data?.envFile || ''
    // Load secret bindings for the .secret file
    let secretContent = ''
    try {
      const br = await composeStackSecretApi.list(id)
      const lines = ((br.data as any) || [])
        .filter((b: any) => b.targetType === 'env' && b.envName)
        .map((b: any) => b.envName)
      if (lines.length) {
        secretContent = '# Secret variables injected from Vault at deploy time.\n# This file lists ONLY the variable names — values live in Vault.\n' + lines.join('\n') + '\n'
      }
    } catch { /* best-effort */ }
    const zip = buildZip([
      { name: 'docker-compose.yml', content },
      ...(envContent ? [{ name: '.env', content: envContent }] : []),
      ...(secretContent ? [{ name: '.secret', content: secretContent }] : []),
    ])
    const blob = new Blob([zip.buffer as ArrayBuffer], { type: 'application/zip' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${name || 'stack'}.zip`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  } catch (e: any) {
    message.error(e?.message || String(e))
  }
}

function confirmRemove(s: ComposeStackSummary) {
  const isExternal = !s.id
  const base = isExternal ? { hostId: s.hostId, name: s.name } : { id: s.id }
  const removeVolumes = ref(false)
  dialog.warning({
    title: t('buttons.delete'),
    content: () => h('div', null, [
      h('p', { style: 'margin:0 0 8px 0' }, t('prompts.delete')),
      h('label', { style: 'display:flex; align-items:center; gap:6px;' }, [
        h('input', {
          type: 'checkbox',
          onChange: (e: Event) => (removeVolumes.value = (e.target as HTMLInputElement).checked),
        }),
        h('span', null, t('fields.remove_volumes') || 'Also remove named volumes'),
      ]),
    ]) as any,
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: () => removeWithForceConfirm(base, removeVolumes.value),
  })
}

const columns = [
  {
    title: t('fields.status'),
    key: "status",
    width: 90,
    sorter: (a: ComposeStackSummary, b: ComposeStackSummary) => (a.status || '').localeCompare(b.status || ''),
    render: (s: ComposeStackSummary) => {
      const type = s.status === 'active' ? 'success' : (s.status === 'partial' ? 'warning' : 'default')
      return renderTag(s.status || '-', type as any)
    }
  },
  {
    title: t('fields.name'),
    key: "name",
    sorter: (a: ComposeStackSummary, b: ComposeStackSummary) => (a.name || '').localeCompare(b.name || ''),
    render: (s: ComposeStackSummary) => {
      if (!s.id) {
        const link = renderLink({ name: 'std_stack_external_detail', params: { hostId: s.hostId, name: s.name } }, s.name)
        const badge = h(NTag, { size: 'small', type: 'default', round: true, style: 'margin-left:6px' }, { default: () => t('fields.external') || 'external' })
        return h('span', null, [link, badge])
      }
      const link = renderLink({ name: 'std_stack_detail', params: { id: s.id } }, s.name)
      const addons = renderAddonBadges(s.activeAddons)
      if (!addons) return link
      return h('span', { style: 'display: inline-flex; align-items: center; gap: 6px; flex-wrap: wrap' }, [link, addons])
    },
  },
  {
    title: t('fields.services') || 'Services',
    key: "services",
    sorter: (a: ComposeStackSummary, b: ComposeStackSummary) => (a.services || 0) - (b.services || 0),
  },
  {
    title: t('fields.running') || 'Running',
    key: "running",
    sorter: (a: ComposeStackSummary, b: ComposeStackSummary) => (a.running || 0) - (b.running || 0),
    render: (s: ComposeStackSummary) => `${s.running}/${s.containers}`,
  },
  {
    title: t('fields.actions'),
    key: "actions",
    width: 260,
    render(s: ComposeStackSummary) {
      if (!s.id) {
        const externalButtons = [
          actionButton('success', PlayOutline, t('buttons.start'), s.status === 'active',
            () => runAction(() => composeStackApi.start({ hostId: s.hostId, name: s.name }), t('buttons.start'))),
          actionButton('warning', StopOutline, t('buttons.stop'), s.status === 'inactive',
            () => runAction(() => composeStackApi.stop({ hostId: s.hostId, name: s.name }), t('buttons.stop'))),
          actionButton('info', EyeOutline, t('buttons.view') || 'Details', false,
            () => router.push({ name: 'std_stack_external_detail', params: { hostId: s.hostId, name: s.name } })),
          actionButton('error', TrashOutline, t('buttons.delete'), false, () => confirmRemove(s)),
        ]
        return h(NButtonGroup, null, { default: () => externalButtons })
      }
      const buttons = [
        actionButton('success', PlayOutline, t('buttons.start'), s.status === 'active',
          () => runAction(() => composeStackApi.start({ id: s.id }), t('buttons.start'))),
        actionButton('warning', StopOutline, t('buttons.stop'), s.status === 'inactive',
          () => runAction(() => composeStackApi.stop({ id: s.id }), t('buttons.stop'))),
        actionButton('info', RefreshOutline, t('buttons.deploy'), false,
          () => runAction(() => composeStackApi.deployById(s.id), t('buttons.deploy'))),
        actionButton('default', CreateOutline, t('buttons.edit'), false,
          () => router.push({ name: 'std_stack_edit', params: { id: s.id } })),
        actionButton('default', DownloadOutline, t('buttons.download') || 'Download', false,
          () => downloadStack(s.id, s.name)),
        actionButton('error', TrashOutline, t('buttons.delete'), false, () => confirmRemove(s)),
      ]
      return h(NButtonGroup, null, { default: () => buttons })
    },
  },
];
const { state, pagination, fetchData, changePage, changePageSize, paginatedData, handleSorterChange, setSortColumns } = useDataTable(composeStackApi.search, filter, { remote: false, autoFetch: false })
setSortColumns(columns)

watch(selectedHostId, (v) => {
  filter.hostId = v || ''
  if (v) fetchData()
})

onMounted(() => {
  if (selectedHostId.value) {
    filter.hostId = selectedHostId.value
    fetchData()
  }
})
</script>
