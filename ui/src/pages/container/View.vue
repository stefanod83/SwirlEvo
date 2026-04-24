<template>
  <x-page-header :subtitle="model.name">
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'container_list' })">
        <template #icon>
          <n-icon>
            <back-icon />
          </n-icon>
        </template>
        {{ t('buttons.return') }}
      </n-button>
      <n-button secondary size="small" @click="fetchData" :loading="loading">
        <template #icon>
          <n-icon>
            <refresh-outline />
          </n-icon>
        </template>
        {{ t('buttons.refresh') }}
      </n-button>
    </template>
  </x-page-header>
  <div class="page-body">
    <n-tabs type="line" style="margin-top: -12px" v-model:value="activeTab">
      <n-tab-pane name="detail" :tab="t('fields.detail')" display-directive="show:lazy">
        <n-space vertical :size="16">
          <x-description label-placement="left" label-align="right">
            <x-description-item :label="t('fields.id')" :span="2">{{ model.id }}</x-description-item>
            <x-description-item :label="t('fields.name')" :span="2">{{ model.name }}</x-description-item>
            <x-description-item :label="t('objects.image')" :span="2">{{ model.image }}</x-description-item>
            <x-description-item label="PID">{{ model.pid }}</x-description-item>
            <x-description-item :label="t('fields.state')">
              <n-tag
                round
                size="small"
                :type="stateTagType(model.state)"
              >{{ model.state }}</n-tag>
            </x-description-item>
            <x-description-item :label="t('fields.created_at')">{{ model.createdAt }}</x-description-item>
            <x-description-item :label="t('fields.started_at')">{{ model.startedAt }}</x-description-item>
            <x-description-item :label="t('fields.ip_address')" :span="2">
              <!-- Per-network IP list. Each endpoint is tagged with the
                   network name because a container often lives on more
                   than one network (frontend + backend overlays, or the
                   default bridge + a compose network). -->
              <template v-if="model.networks && model.networks.length">
                <n-space :size="6" :wrap="true">
                  <n-tag
                    v-for="n in model.networks"
                    :key="n.name"
                    size="small"
                    round
                    :bordered="false"
                    type="info"
                  >
                    {{ n.name }}: <code style="margin-left: 4px;">{{ n.ip || '—' }}</code>
                    <span v-if="n.ipv6" style="margin-left: 6px; opacity: 0.7;">({{ n.ipv6 }})</span>
                  </n-tag>
                </n-space>
              </template>
              <span v-else style="opacity: 0.6;">—</span>
            </x-description-item>
            <x-description-item :label="t('fields.published_ports')" :span="2">
              <!-- Host-published ports only. Ports with no publicPort
                   (purely intra-compose) are excluded since they are
                   not reachable from outside the Docker daemon. -->
              <template v-if="publishedPorts.length">
                <n-space :size="6" :wrap="true">
                  <n-tag
                    v-for="p in publishedPorts"
                    :key="p.key"
                    size="small"
                    round
                    :bordered="false"
                    type="success"
                  >
                    <span v-if="p.host">{{ p.host }}:</span>{{ p.publicPort }} &rarr; {{ p.privatePort }}/{{ p.type }}
                  </n-tag>
                </n-space>
              </template>
              <span v-else style="opacity: 0.6;">—</span>
            </x-description-item>
          </x-description>
          <!-- Resources: runtime limits/reservations the Docker
               daemon enforces on this container. Populated only when
               the compose stack or docker-run sets
               HostConfig.Resources; absent for unlimited containers.
               Byte values are humanised via formatBytes.
               Using naive-ui n-descriptions directly (instead of
               XDescription) because long labels like "Memory
               reservation" wrap mid-word inside the custom grid
               layout — n-descriptions gives us a proper bordered
               key/value table that scales to narrow screens. -->
          <x-panel :title="t('container.resources_title')" v-if="model.resources">
            <n-descriptions
              :column="2"
              size="small"
              label-placement="left"
              bordered
            >
              <n-descriptions-item v-if="model.resources.cpus" :label="t('container.cpu_limit')">
                {{ model.resources.cpus }} CPU
              </n-descriptions-item>
              <n-descriptions-item v-if="model.resources.cpuShares" :label="t('container.cpu_shares')">
                <n-space :size="6" align="center" :wrap="false" style="flex-wrap: wrap">
                  <code>{{ model.resources.cpuShares }}</code>
                  <span style="font-size: 12px; opacity: 0.7">{{ t('container.cpu_shares_hint') }}</span>
                </n-space>
              </n-descriptions-item>
              <n-descriptions-item v-if="model.resources.memory" :label="t('container.memory_limit')">
                {{ formatBytes(model.resources.memory) }}
              </n-descriptions-item>
              <n-descriptions-item v-if="model.resources.memoryReservation" :label="t('container.memory_reservation')">
                {{ formatBytes(model.resources.memoryReservation) }}
              </n-descriptions-item>
              <n-descriptions-item v-if="model.resources.memorySwap" :label="t('container.memory_swap')">
                <span v-if="model.resources.memorySwap === -1">{{ t('container.swap_unlimited') }}</span>
                <span v-else>{{ formatBytes(model.resources.memorySwap) }}</span>
              </n-descriptions-item>
              <n-descriptions-item v-if="model.resources.pidsLimit" :label="t('container.pids_limit')">
                {{ model.resources.pidsLimit }}
              </n-descriptions-item>
            </n-descriptions>
          </x-panel>
          <x-panel :title="t('fields.labels')" v-if="model.labels && model.labels.length">
            <n-table size="small" :bordered="true" :single-line="false">
              <thead>
                <tr>
                  <th>{{ t('fields.name') }}</th>
                  <th>{{ t('fields.value') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="label in model.labels">
                  <td>{{ label.name }}</td>
                  <td>{{ label.value }}</td>
                </tr>
              </tbody>
            </n-table>
          </x-panel>
        </n-space>
      </n-tab-pane>
      <n-tab-pane name="raw" :tab="t('fields.raw')" display-directive="show:lazy">
        <x-code :code="raw" language="json" />
      </n-tab-pane>
      <n-tab-pane name="logs" :tab="t('fields.logs')" display-directive="show:lazy">
        <x-logs
          type="container"
          :node="node"
          :id="model.id"
          v-if="store.getters.allow('container.logs')"
        ></x-logs>
        <n-alert type="info" v-else>{{ t('texts.403') }}</n-alert>
      </n-tab-pane>
      <n-tab-pane name="stats" :tab="t('fields.stats')" display-directive="show:lazy">
        <!-- Live-ish stats view. Polls containerApi.stats every 2s so
             operators can watch CPU / memory / network trending on a
             single running container. The backend hits Docker's native
             /containers/<id>/stats endpoint with stream=false — no
             Prometheus dependency, works on every daemon. -->
        <x-container-stats :node="node" :id="model.id" v-if="model.id" />
      </n-tab-pane>
      <n-tab-pane name="exec" :tab="t('fields.execute')" display-directive="show:lazy">
        <execute :node="node" :id="model.id" v-if="store.getters.allow('container.execute')"></execute>
        <n-alert type="info" v-else>{{ t('texts.403') }}</n-alert>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, computed, watch } from "vue";
import {
  NButton,
  NTag,
  NSpace,
  NIcon,
  NTable,
  NTabs,
  NTabPane,
  NAlert,
  NDescriptions,
  NDescriptionsItem,
} from "naive-ui";
import { ArrowBackCircleOutline as BackIcon, RefreshOutline } from "@vicons/ionicons5";
import { useStore } from "vuex";
import XPageHeader from "@/components/PageHeader.vue";
import XCode from "@/components/Code.vue";
import XPanel from "@/components/Panel.vue";
import XLogs from "@/components/Logs.vue";
import XContainerStats from "@/components/ContainerStats.vue";
import { XDescription, XDescriptionItem } from "@/components/description";
import containerApi from "@/api/container";
import type { Container } from "@/api/container";
import { useRoute, useRouter } from "vue-router";
import Execute from "./modules/Execute.vue";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute();
const router = useRouter();
const store = useStore();
const model = ref({} as Container);
const raw = ref('');
const loading = ref(false);
const node = route.params.node as string || '';

// Tab name is driven by `?tab=` so the container list's Quick Actions
// (logs / inspect / stats / console) can deep-link straight to a panel.
// We mirror changes back into the URL so page refresh keeps the user
// on the same tab.
const VALID_TABS = ['detail', 'raw', 'logs', 'stats', 'exec'] as const
type TabName = typeof VALID_TABS[number]
const initialTab = (() => {
  const q = route.query.tab as string | undefined
  return (q && (VALID_TABS as readonly string[]).includes(q) ? q : 'detail') as TabName
})()
const activeTab = ref<TabName>(initialTab)
watch(activeTab, (t) => {
  if (route.query.tab !== t) {
    router.replace({ ...route, query: { ...route.query, tab: t } })
  }
})

// publishedPorts dedups `{public, private, type}` triplets (Docker
// emits one entry per listen address, so 0.0.0.0 + :: for the same
// mapping double-count) and filters out intra-compose ports that have
// no publicPort mapping.
const publishedPorts = computed(() => {
  const seen = new Map<string, {
    key: string; host: string; publicPort: number; privatePort: number; type: string
  }>()
  for (const p of model.value?.ports || []) {
    if (!p.publicPort || !p.privatePort) continue
    const type = p.type || 'tcp'
    // Key ignores the host address so 0.0.0.0 vs :: dedup, but we
    // keep the first non-wildcard IP we see as a host prefix so
    // operators spot bind-address-specific publishes.
    const key = `${p.publicPort}:${p.privatePort}/${type}`
    const host = p.ip && p.ip !== '0.0.0.0' && p.ip !== '::' ? p.ip : ''
    if (!seen.has(key)) {
      seen.set(key, { key, host, publicPort: p.publicPort, privatePort: p.privatePort, type })
    } else if (host && !seen.get(key)!.host) {
      seen.get(key)!.host = host
    }
  }
  return [...seen.values()]
})

// stateTagType mirrors ContainerTable.vue's healthcheck-aware mapping:
// healthy/running → success, starting/paused → warning, everything
// else (exited, unhealthy, dead, …) → error.
function stateTagType(state: string): 'success' | 'warning' | 'error' {
  switch (state) {
    case 'healthy':
    case 'running':
      return 'success'
    case 'starting':
    case 'paused':
      return 'warning'
    default:
      return 'error'
  }
}

// formatBytes humanises a raw byte count into the unit the operator
// originally typed in the compose wizard. Docker exposes Memory as
// int64 bytes, so the caller never sees "256M" back — we reconstruct
// a readable representation that matches what `docker stats` shows.
function formatBytes(n: number): string {
  if (!n || n <= 0) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  // Whole numbers render without decimals (2 GiB), fractional values
  // show two digits (1.50 GiB).
  const s = v === Math.floor(v) ? String(v) : v.toFixed(2)
  return `${s} ${units[i]}`
}

async function fetchData() {
  loading.value = true;
  try {
    const id = route.params.id as string;
    let r = await containerApi.find(node, id);
    model.value = r.data?.container as Container;
    raw.value = r.data?.raw as string;
  } finally {
    loading.value = false;
  }
}

onMounted(fetchData);
</script>