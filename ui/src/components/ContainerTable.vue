<template>
  <!--
    ContainerTable accepts the FULL dataset and handles sort + pagination
    internally so that sort is global across every page, not just the
    visible rows. The parent must pass `data` containing every container
    (no external pagination) and an optional `pagination` prop to render
    the page picker. The older "remote" external-pagination pattern has
    been removed — see `ui/src/utils/data-table.ts` and
    `feedback_naive_ui_remote_sort.md` for the rationale.

    Per-row action buttons have been replaced by a compact "Quick Actions"
    column (logs / inspect / stats / console). Lifecycle actions (start,
    stop, kill, restart, pause, resume, remove) live on the parent List
    as bulk operations driven by the row-selection checkbox.
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
  NDataTable, NButton, NButtonGroup, NIcon, NTooltip, NText, NTag,
} from "naive-ui";
import {
  DocumentTextOutline as LogsIcon,
  InformationCircleOutline as InspectIcon,
  PulseOutline as StatsIcon,
  TerminalOutline as ConsoleIcon,
} from "@vicons/ionicons5";
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
  // When true the "Stats" quick-action is rendered (gated on the
  // parent-side metrics-enabled check). Logs / Inspect / Console are
  // always rendered because they are native Docker operations.
  metricsEnabled?: boolean;
}>()

const emit = defineEmits<{
  (e: 'refresh'): void;
  (e: 'update:page', p: number): void;
  (e: 'update-page-size', s: number): void;
  (e: 'update:checkedKeys', keys: string[]): void;
}>()

const { t } = useI18n()
const router = useRouter()

function quickActionBtn(iconCmp: any, tooltip: string, onClick: () => void) {
  return h(NTooltip, { trigger: 'hover' }, {
    trigger: () => h(NButton, {
      size: 'tiny',
      quaternary: true,
      onClick,
    }, { icon: () => h(NIcon, null, { default: () => h(iconCmp) }) }),
    default: () => tooltip,
  })
}

function projectOf(c: Container): string {
  return c.labels?.find(l => l.name === 'com.docker.compose.project')?.value || ''
}

// Pick the first non-empty IPv4 endpoint from the container's network
// list. Falls back to the legacy `ports[*].ip` entry when no explicit
// network info is available (older backends).
function primaryIp(c: Container): string {
  const n = (c.networks || []).find(n => !!n.ip)
  if (n?.ip) return n.ip
  const p = (c.ports || []).find(p => !!p.ip && p.ip !== '0.0.0.0' && p.ip !== '::')
  return p?.ip || ''
}

// Render the published ports column as a vertically-stacked list of
// "<publicPort>:<privatePort>/<type>" pairs. Unpublished ports (no
// publicPort) are omitted — the list is for "what's reachable from
// outside the container" not the full exposed set.
function renderPublishedPorts(c: Container) {
  const pubs = (c.ports || [])
    .filter(p => p.publicPort && p.privatePort)
    // Dedup by (public, private, type) — Docker reports one entry per
    // listen address (0.0.0.0 + ::) and we want to collapse them.
    .reduce((acc, p) => {
      const key = `${p.publicPort}:${p.privatePort}/${p.type || 'tcp'}`
      if (!acc.find(a => a === key)) acc.push(key)
      return acc
    }, [] as string[])
  if (pubs.length === 0) return h(NText, { depth: 3 }, { default: () => '—' })
  return h('div', { style: 'display: flex; flex-direction: column; gap: 2px;' },
    pubs.map(p => h(NTag, { size: 'tiny', bordered: false, type: 'info' }, { default: () => p }))
  )
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

// Build the available state filter values (collected from the dataset so
// we don't surface empty options). Naive UI's column-level filter renders
// a dropdown above the state column with a checkbox list.
const stateFilterOptions = computed(() => {
  const set = new Set<string>()
  for (const c of props.data || []) {
    if (c.state) set.add(c.state)
  }
  return [...set].sort().map(s => ({ label: s, value: s }))
})

const columns = computed(() => {
  const base: any[] = [
    {
      title: t('fields.state'),
      key: "state",
      fixed: "left" as const,
      filter: stateFilterOptions.value.length
        ? ((value: any, row: Container) => row.state === value)
        : undefined,
      filterOptions: stateFilterOptions.value.length
        ? stateFilterOptions.value
        : undefined,
      filterMultiple: true,
      sorter: (a: Container, b: Container) => (a.state || '').localeCompare(b.state || ''),
      render(c: Container) {
        // Healthcheck-aware state tag:
        //   healthy   → green   (running + healthcheck passing)
        //   running   → green   (running, no healthcheck)
        //   starting  → warning (healthcheck warm-up window)
        //   paused    → warning
        //   unhealthy → red
        //   everything else (exited, dead, …) → red
        let type: 'success' | 'warning' | 'error' = 'error'
        switch (c.state) {
          case 'healthy':
          case 'running':
            type = 'success'; break
          case 'starting':
          case 'paused':
            type = 'warning'; break
        }
        return renderTag(c.state, type as any)
      },
    },
    {
      title: t('fields.name'),
      key: "name",
      sorter: (a: Container, b: Container) => (a.name || '').localeCompare(b.name || ''),
      render: (c: Container) => {
        const node = c.labels?.find(l => l.name === 'com.docker.swarm.node.id')
        // Manual truncation instead of `ellipsis: { tooltip: true }`.
        // Ellipsis requires a column width to kick in, and giving Name
        // a width in a `scroll-x="max-content"` table alongside a
        // fixed-left `state` column collapses the rest of the layout
        // (see feedback_naive_ui_fixed_ellipsis_layout.md).
        const name = c.name && c.name.length > 32
          ? c.name.substring(0, 32) + '…'
          : c.name
        return renderLink(
          { name: 'container_detail', params: { id: c.id, node: node?.value || props.node || '-' } },
          name,
        )
      },
    },
    {
      title: t('fields.quick_actions'),
      key: "quick_actions",
      render: (c: Container) => {
        const node = c.labels?.find(l => l.name === 'com.docker.swarm.node.id')?.value
          || props.node || '-'
        const goTab = (tab: string) => router.push({
          name: 'container_detail',
          params: { id: c.id, node },
          query: { tab },
        })
        const buttons = [
          quickActionBtn(LogsIcon, t('fields.logs'), () => goTab('logs')),
          quickActionBtn(InspectIcon, t('fields.inspect'), () => goTab('detail')),
        ]
        if (props.metricsEnabled) {
          buttons.push(quickActionBtn(StatsIcon, t('fields.stats'), () => goTab('stats')))
        }
        buttons.push(quickActionBtn(ConsoleIcon, t('fields.execute'), () => goTab('exec')))
        return h(NButtonGroup, null, { default: () => buttons })
      },
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
      title: t('objects.image'),
      key: "image",
      sorter: (a: Container, b: Container) => (a.image || '').localeCompare(b.image || ''),
      render: (c: Container) => {
        const img = c.image || ''
        return img.length > 60 ? img.substring(0, 60) + '…' : img
      },
    },
    {
      title: t('fields.created_at'),
      key: "createdAt",
      sorter: (a: Container, b: Container) => (a.createdAt || '').localeCompare(b.createdAt || ''),
    },
    {
      title: t('fields.ip_address'),
      key: "ip",
      sorter: (a: Container, b: Container) => primaryIp(a).localeCompare(primaryIp(b)),
      render: (c: Container) => {
        const ip = primaryIp(c)
        if (!ip) return h(NText, { depth: 3 }, { default: () => '—' })
        return h('code', { style: 'font-size: 12px;' }, ip)
      },
    },
    {
      title: t('fields.published_ports'),
      key: "ports",
      render: (c: Container) => renderPublishedPorts(c),
    },
  )
  return base
})
</script>
