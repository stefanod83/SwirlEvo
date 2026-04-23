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
    <n-tabs type="line" style="margin-top: -12px">
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
          </x-description>
          <!-- Resources: runtime limits/reservations the Docker
               daemon enforces on this container. Populated only when
               the compose stack or docker-run sets
               HostConfig.Resources; absent for unlimited containers.
               Byte values are humanised via formatBytes. -->
          <x-panel :title="t('container.resources_title')" v-if="model.resources">
            <x-description label-placement="left" label-align="right">
              <x-description-item v-if="model.resources.cpus" :label="t('container.cpu_limit')">
                {{ model.resources.cpus }} CPU
              </x-description-item>
              <x-description-item v-if="model.resources.cpuShares" :label="t('container.cpu_shares')">
                {{ model.resources.cpuShares }}
                <span style="margin-left: 6px; font-size: 12px; opacity: 0.7">{{ t('container.cpu_shares_hint') }}</span>
              </x-description-item>
              <x-description-item v-if="model.resources.memory" :label="t('container.memory_limit')">
                {{ formatBytes(model.resources.memory) }}
              </x-description-item>
              <x-description-item v-if="model.resources.memoryReservation" :label="t('container.memory_reservation')">
                {{ formatBytes(model.resources.memoryReservation) }}
              </x-description-item>
              <x-description-item v-if="model.resources.memorySwap" :label="t('container.memory_swap')">
                <span v-if="model.resources.memorySwap === -1">{{ t('container.swap_unlimited') }}</span>
                <span v-else>{{ formatBytes(model.resources.memorySwap) }}</span>
              </x-description-item>
              <x-description-item v-if="model.resources.pidsLimit" :label="t('container.pids_limit')">
                {{ model.resources.pidsLimit }}
              </x-description-item>
            </x-description>
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
      <n-tab-pane name="exec" :tab="t('fields.execute')" display-directive="show:lazy">
        <execute :node="node" :id="model.id" v-if="store.getters.allow('container.execute')"></execute>
        <n-alert type="info" v-else>{{ t('texts.403') }}</n-alert>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  NButton,
  NTag,
  NSpace,
  NIcon,
  NTable,
  NTabs,
  NTabPane,
  NAlert,
} from "naive-ui";
import { ArrowBackCircleOutline as BackIcon, RefreshOutline } from "@vicons/ionicons5";
import { useStore } from "vuex";
import XPageHeader from "@/components/PageHeader.vue";
import XCode from "@/components/Code.vue";
import XPanel from "@/components/Panel.vue";
import XLogs from "@/components/Logs.vue";
import { XDescription, XDescriptionItem } from "@/components/description";
import containerApi from "@/api/container";
import type { Container } from "@/api/container";
import { useRoute } from "vue-router";
import Execute from "./modules/Execute.vue";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute();
const store = useStore();
const model = ref({} as Container);
const raw = ref('');
const loading = ref(false);
const node = route.params.node as string || '';

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