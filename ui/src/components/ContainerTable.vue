<template>
  <!--
    ContainerTable accepts the FULL dataset and handles sort + pagination
    internally so that sort is global across every page, not just the
    visible rows. The parent must pass `data` containing every container
    (no external pagination) and an optional `pagination` prop to render
    the page picker. The older "remote" external-pagination pattern has
    been removed — see `ui/src/utils/data-table.ts` and
    `feedback_naive_ui_remote_sort.md` for the rationale.
  -->
  <n-data-table
    remote
    :row-key="(row: any) => row.id"
    size="small"
    :columns="allColumns"
    :data="paginatedData"
    :pagination="internalPagination"
    :loading="loading"
    :checked-row-keys="selectable ? checkedKeys : undefined"
    @update:checked-row-keys="(k: any) => $emit('update:checkedKeys', k)"
    @update:page="onPageChange"
    @update-page-size="onPageSizeChange"
    @update:sorter="handleSorterChange"
    scroll-x="max-content"
  />
</template>

<script setup lang="ts">
import { h, computed, ref, watch } from "vue";
import {
  NDataTable, NButton, NButtonGroup, NIcon, NTooltip, NCheckbox, NSpace, NText,
  useDialog, useMessage,
} from "naive-ui";
import {
  PlayOutline, StopOutline, RefreshOutline, PauseOutline,
  FlashOffOutline, TrashOutline, EyeOutline,
} from "@vicons/ionicons5";
import containerApi from "@/api/container";
import type { Container } from "@/api/container";
import { renderLink, renderTag } from "@/utils/render";
import { useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  node: string;
  data: Container[];
  loading?: boolean;
  pagination?: any;
  showStackColumn?: boolean;
  selectable?: boolean;
  checkedKeys?: string[];
}>()

const emit = defineEmits<{
  (e: 'refresh'): void;
  (e: 'update:page', p: number): void;
  (e: 'update-page-size', s: number): void;
  (e: 'update:checkedKeys', keys: string[]): void;
}>()

const { t } = useI18n()
const router = useRouter()
const dialog = useDialog()
const message = useMessage()

function actionButton(type: 'default' | 'error' | 'warning' | 'success' | 'info', iconCmp: any, tooltip: string, disabled: boolean, onClick: () => void) {
  return h(NTooltip, { trigger: 'hover' }, {
    trigger: () => h(NButton, { size: 'tiny', quaternary: true, type, disabled, onClick }, { icon: () => h(NIcon, null, { default: () => h(iconCmp) }) }),
    default: () => tooltip,
  })
}

async function runAction(fn: () => Promise<any>, msg: string) {
  try { await fn(); message.success(msg); emit('refresh') }
  catch (e: any) { message.error(e?.message || String(e)) }
}

function confirmDelete(c: Container) {
  // Dedicated reactive flag per dialog — the checkbox writes to it and the
  // positive-click handler reads it at submit time, so unchecked stays the
  // safe default and the user has to opt in explicitly to dropping
  // anonymous volumes. Named volumes are never touched by this flag; see
  // docker.ContainerRemove comment.
  const removeVolumes = ref(false)
  dialog.warning({
    title: t('buttons.delete'),
    content: () => h(NSpace, { vertical: true, size: 8 }, {
      default: () => [
        h(NText, null, { default: () => t('prompts.delete') }),
        h(NCheckbox, {
          checked: removeVolumes.value,
          'onUpdate:checked': (v: boolean) => { removeVolumes.value = v },
        }, { default: () => t('prompts.remove_anonymous_volumes') }),
        h(NText, { depth: 3, style: 'font-size:12px' }, {
          default: () => t('tips.remove_anonymous_volumes'),
        }),
      ],
    }),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: () => runAction(
      () => containerApi.delete(props.node, c.id, c.name, removeVolumes.value),
      t('buttons.delete'),
    ),
  })
}

function projectOf(c: Container): string {
  return c.labels?.find(l => l.name === 'com.docker.compose.project')?.value || ''
}

const allColumns = computed(() => {
  const sel = props.selectable ? [{ type: 'selection' as const, fixed: 'left' as const }] : []
  return [...sel, ...columns.value]
})

// Global client-side sort over the FULL dataset passed in via `props.data`.
// Because this component is the sole owner of the column definitions, it is
// also the owner of the sort state and the pagination slice.
const sorterState = ref<{ columnKey: string | number, order: 'ascend' | 'descend' | false } | null>(null)
function handleSorterChange(s: any) {
  if (!s || !s.order) { sorterState.value = null }
  else { sorterState.value = { columnKey: s.columnKey, order: s.order } }
  // Reset to page 1 so the user sees the new top of the ordering.
  localPage.value = 1
}
const sortedData = computed(() => {
  const s = sorterState.value
  if (!s || !s.order) return props.data
  const col = columns.value.find((c: any) => c && c.key === s.columnKey)
  const fn: any = col?.sorter
  if (typeof fn !== 'function') return props.data
  const copy = [...props.data]
  copy.sort((a, b) => {
    const r = fn(a, b)
    return s.order === 'ascend' ? r : -r
  })
  return copy
})

// Pagination state. If the parent provides a `pagination` prop we mirror
// its page/pageSize into a local ref so sort changes can reset it without
// mutating the parent's reactive object. If no `pagination` is given, we
// show the entire dataset at once (no page picker).
const localPage = ref(1)
const internalPagination = computed(() => {
  if (!props.pagination) return false as false
  const pageSize = props.pagination.pageSize || 10
  const itemCount = sortedData.value.length
  return {
    ...props.pagination,
    page: localPage.value,
    pageSize,
    itemCount,
    pageCount: Math.max(1, Math.ceil(itemCount / pageSize)),
  }
})
const paginatedData = computed(() => {
  if (!props.pagination) return sortedData.value
  const pageSize = props.pagination.pageSize || 10
  const start = (localPage.value - 1) * pageSize
  return sortedData.value.slice(start, start + pageSize)
})
function onPageChange(p: number) {
  localPage.value = p
  emit('update:page', p)
}
function onPageSizeChange(s: number) {
  localPage.value = 1
  if (props.pagination) props.pagination.pageSize = s
  emit('update-page-size', s)
}
// Reset page when the dataset changes (e.g. after a filter change). Avoids
// a "ghost empty page" feeling where the user lands on page 3 of an empty
// result set.
watch(() => props.data, () => { localPage.value = 1 })

const columns = computed(() => {
  const base: any[] = [
    {
      title: t('fields.state'),
      key: "state",
      fixed: "left" as const,
      width: 90,
      sorter: (a: Container, b: Container) => (a.state || '').localeCompare(b.state || ''),
      render(c: Container) {
        const type = c.state === 'running' ? 'success' : (c.state === 'paused' ? 'warning' : 'error')
        return renderTag(c.state, type as any)
      }
    },
    {
      title: t('fields.name'),
      key: "name",
      sorter: (a: Container, b: Container) => (a.name || '').localeCompare(b.name || ''),
      render: (c: Container) => {
        const node = c.labels?.find(l => l.name === 'com.docker.swarm.node.id')
        const name = c.name.length > 32 ? c.name.substring(0, 32) + '...' : c.name
        return renderLink({ name: 'container_detail', params: { id: c.id, node: node?.value || props.node || '-' } }, name)
      },
    },
    {
      title: t('objects.image'),
      key: "image",
      sorter: (a: Container, b: Container) => (a.image || '').localeCompare(b.image || ''),
    },
  ]
  if (props.showStackColumn) {
    base.push({
      title: t('objects.stack'),
      key: "stack",
      sorter: (a: Container, b: Container) => projectOf(a).localeCompare(projectOf(b)),
      render: (c: Container) => {
        const project = projectOf(c)
        if (!project) return ''
        return renderLink({ name: 'std_stack_external_detail', params: { hostId: props.node, name: project } }, project)
      },
    })
  }
  base.push(
    {
      title: t('fields.status'),
      key: "status",
      sorter: (a: Container, b: Container) => (a.status || '').localeCompare(b.status || ''),
    },
    {
      title: t('fields.created_at'),
      key: "createdAt",
      sorter: (a: Container, b: Container) => (a.createdAt || '').localeCompare(b.createdAt || ''),
    },
    {
      title: t('fields.actions'),
      key: "actions",
      width: 260,
      render(c: Container) {
        const running = c.state === 'running'
        const paused = c.state === 'paused'
        const buttons = [
          actionButton('success', PlayOutline, t('buttons.start'), running || paused,
            () => runAction(() => containerApi.start(props.node, c.id, c.name), t('buttons.start'))),
          actionButton('warning', StopOutline, t('buttons.stop'), !running,
            () => runAction(() => containerApi.stop(props.node, c.id, c.name), t('buttons.stop'))),
          actionButton('info', RefreshOutline, t('buttons.restart'), !running,
            () => runAction(() => containerApi.restart(props.node, c.id, c.name), t('buttons.restart'))),
          actionButton('warning', PauseOutline, paused ? t('buttons.unpause') : t('buttons.pause'), !running && !paused,
            () => runAction(() => paused ? containerApi.unpause(props.node, c.id, c.name) : containerApi.pause(props.node, c.id, c.name), paused ? t('buttons.unpause') : t('buttons.pause'))),
          actionButton('error', FlashOffOutline, t('buttons.kill'), !running,
            () => runAction(() => containerApi.kill(props.node, c.id, c.name), t('buttons.kill'))),
          actionButton('default', EyeOutline, t('buttons.view'), false,
            () => router.push({ name: 'container_detail', params: { id: c.id, node: props.node || '-' } })),
          actionButton('error', TrashOutline, t('buttons.delete'), running || paused, () => confirmDelete(c)),
        ]
        return h(NButtonGroup, null, { default: () => buttons })
      },
    }
  )
  return base
})
</script>
