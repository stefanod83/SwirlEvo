<template>
  <!--
    ContainerStats — lightweight live view over Docker's native
    /containers/<id>/stats endpoint (stream=false, called every 2s).
    No Prometheus dependency: works on every Docker daemon, including
    hosts that have never had the /metric/prometheus setting filled in.
    -->
  <n-space vertical :size="12">
    <n-space :size="8" align="center">
      <n-switch v-model:value="live" size="small" />
      <span>{{ live ? t('container.stats_live_on') : t('container.stats_live_off') }}</span>
      <n-button size="tiny" secondary @click="refresh" :loading="loading">
        <template #icon><n-icon><refresh-outline /></n-icon></template>
        {{ t('buttons.refresh') }}
      </n-button>
      <n-text v-if="updatedAt" depth="3" style="font-size: 12px;">
        {{ t('container.stats_updated') }}: {{ updatedAt }}
      </n-text>
    </n-space>
    <n-alert v-if="error" type="error">{{ error }}</n-alert>
    <n-descriptions v-if="data" :column="2" size="small" label-placement="left" bordered>
      <n-descriptions-item :label="t('container.cpu_percent')">
        {{ data.cpuPercent }}
      </n-descriptions-item>
      <n-descriptions-item :label="t('container.mem_usage')">
        {{ data.memUsage }} / {{ data.memLimit }}
        <span v-if="data.memPercent" style="opacity: 0.7"> ({{ data.memPercent }})</span>
      </n-descriptions-item>
      <n-descriptions-item :label="t('container.net_io')">
        ↓ {{ data.netRx }} / ↑ {{ data.netTx }}
      </n-descriptions-item>
      <n-descriptions-item :label="t('container.block_io')">
        r {{ data.blockRead }} / w {{ data.blockWrite }}
      </n-descriptions-item>
      <n-descriptions-item :label="t('container.pids')">
        {{ data.pids }}
      </n-descriptions-item>
    </n-descriptions>
    <n-empty v-else-if="!loading" :description="t('container.stats_unavailable')" />
  </n-space>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import {
  NSpace, NSwitch, NButton, NIcon, NText, NAlert,
  NDescriptions, NDescriptionsItem, NEmpty,
} from 'naive-ui'
import { RefreshOutline } from '@vicons/ionicons5'
import containerApi from '@/api/container'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ node: string; id: string }>()
const { t } = useI18n()

interface StatsView {
  cpuPercent: string
  memUsage: string
  memLimit: string
  memPercent: string
  netRx: string
  netTx: string
  blockRead: string
  blockWrite: string
  pids: string
}

const data = ref<StatsView | null>(null)
const loading = ref(false)
const error = ref('')
const updatedAt = ref('')
const live = ref(true)
let timer: any = null

function formatBytes(n: number): string {
  if (!isFinite(n) || n <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  const s = v === Math.floor(v) ? String(v) : v.toFixed(2)
  return `${s} ${units[i]}`
}

function percent(n: number): string {
  if (!isFinite(n) || n < 0) return '0.00%'
  return `${n.toFixed(2)}%`
}

// Docker's raw stats payload is verbose. We derive the same five
// columns `docker stats` renders:
//
//   CPU%    = (cpu_delta / system_delta) * online_cpus * 100
//   MEM     = memory_stats.usage - cache (if present, bytes)
//   MEM%    = (mem / limit) * 100
//   NET I/O = sum of all interfaces rx_bytes / tx_bytes
//   BLOCK   = sum of io_service_bytes_recursive Read / Write
//   PIDS    = pids_stats.current
function deriveStats(raw: any): StatsView {
  const cpuDelta = (raw?.cpu_stats?.cpu_usage?.total_usage || 0)
    - (raw?.precpu_stats?.cpu_usage?.total_usage || 0)
  const sysDelta = (raw?.cpu_stats?.system_cpu_usage || 0)
    - (raw?.precpu_stats?.system_cpu_usage || 0)
  const onlineCpus = raw?.cpu_stats?.online_cpus
    || raw?.cpu_stats?.cpu_usage?.percpu_usage?.length
    || 1
  const cpuPct = sysDelta > 0 && cpuDelta > 0
    ? (cpuDelta / sysDelta) * onlineCpus * 100
    : 0

  const memUsageRaw = raw?.memory_stats?.usage || 0
  const memCache = raw?.memory_stats?.stats?.cache
    || raw?.memory_stats?.stats?.inactive_file
    || 0
  const mem = Math.max(0, memUsageRaw - memCache)
  const memLimit = raw?.memory_stats?.limit || 0
  const memPct = memLimit > 0 ? (mem / memLimit) * 100 : 0

  let netRx = 0, netTx = 0
  const networks = raw?.networks || {}
  for (const k of Object.keys(networks)) {
    netRx += networks[k]?.rx_bytes || 0
    netTx += networks[k]?.tx_bytes || 0
  }

  let br = 0, bw = 0
  const blk = raw?.blkio_stats?.io_service_bytes_recursive || []
  for (const e of blk) {
    if (e.op === 'Read') br += e.value || 0
    else if (e.op === 'Write') bw += e.value || 0
  }

  return {
    cpuPercent: percent(cpuPct),
    memUsage: formatBytes(mem),
    memLimit: formatBytes(memLimit),
    memPercent: percent(memPct),
    netRx: formatBytes(netRx),
    netTx: formatBytes(netTx),
    blockRead: formatBytes(br),
    blockWrite: formatBytes(bw),
    pids: String(raw?.pids_stats?.current || 0),
  }
}

async function refresh() {
  if (!props.id) return
  loading.value = true
  error.value = ''
  try {
    const r = await containerApi.stats(props.node, props.id)
    if (r.data) {
      data.value = deriveStats(r.data)
      updatedAt.value = new Date().toLocaleTimeString()
    }
  } catch (e: any) {
    error.value = e?.message || String(e)
    data.value = null
  } finally {
    loading.value = false
  }
}

function schedule() {
  if (timer) { clearInterval(timer); timer = null }
  if (live.value) {
    timer = setInterval(refresh, 2000)
  }
}

watch(live, schedule)
watch(() => props.id, () => { refresh(); schedule() })

onMounted(() => { refresh(); schedule() })
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>
