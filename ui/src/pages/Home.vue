<template>
  <n-space class="page-body" vertical :size="12">
    <n-grid cols="1 s:2 m:4" x-gap="12" y-gap="12" responsive="screen">
      <template v-if="isStandalone">
        <n-gi>
          <x-statistic :title="t('objects.host', 2)">
            <template #icon>
              <server-outline />
            </template>
            <x-anchor :url="{ name: 'host_list' }">{{ summary.hostCount }}</x-anchor>
          </x-statistic>
        </n-gi>
        <n-gi>
          <x-statistic :title="t('objects.container', 2)">
            <template #icon>
              <cube-outline />
            </template>
            <x-anchor :url="{ name: 'std_container_list' }">{{ summary.containerCount }}</x-anchor>
          </x-statistic>
        </n-gi>
        <n-gi>
          <x-statistic :title="t('objects.stack', 2)">
            <template #icon>
              <albums-outline />
            </template>
            <x-anchor :url="{ name: 'std_stack_list' }">{{ summary.stackCount }}</x-anchor>
          </x-statistic>
        </n-gi>
        <n-gi>
          <x-statistic :title="t('objects.image', 2)">
            <template #icon>
              <layers-outline />
            </template>
            <x-anchor :url="{ name: 'image_list' }">{{ summary.imageCount }}</x-anchor>
          </x-statistic>
        </n-gi>
      </template>
      <template v-else>
        <n-gi>
          <x-statistic :title="t('objects.node', 2)">
            <template #icon>
              <server-outline />
            </template>
            <x-anchor :url="{ name: 'node_list' }">{{ summary.nodeCount }}</x-anchor>
          </x-statistic>
        </n-gi>
        <n-gi>
          <x-statistic :title="t('objects.network', 2)">
            <template #icon>
              <globe-outline />
            </template>
            <x-anchor :url="{ name: 'network_list' }">{{ summary.networkCount }}</x-anchor>
          </x-statistic>
        </n-gi>
        <n-gi>
          <x-statistic :title="t('objects.service', 2)">
            <template #icon>
              <image-outline />
            </template>
            <x-anchor :url="{ name: 'service_list' }">{{ summary.serviceCount }}</x-anchor>
          </x-statistic>
        </n-gi>
        <n-gi>
          <x-statistic :title="t('objects.stack', 2)">
            <template #icon>
              <albums-outline />
            </template>
            <x-anchor :url="{ name: 'stack_list' }">{{ summary.stackCount }}</x-anchor>
          </x-statistic>
        </n-gi>
      </template>
    </n-grid>
    <n-hr style="margin: 4px 0" />
    <x-dashboard type="home" v-if="!isStandalone" />
  </n-space>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  NSpace,
  NGrid,
  NGi,
  NHr,
} from "naive-ui";
import {
  ServerOutline,
  GlobeOutline,
  ImageOutline,
  AlbumsOutline,
  CubeOutline,
  LayersOutline,
} from "@vicons/ionicons5";
import XStatistic from "@/components/Statistic.vue";
import XAnchor from "@/components/Anchor.vue";
import XDashboard from "@/components/Dashboard.vue";
import systemApi from "@/api/system";
import type { Summary } from "@/api/system";
import { useStore } from "vuex";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const store = useStore()
const isStandalone = computed(() => store.state.mode === 'standalone')
const summary = ref({
  nodeCount: 0,
  networkCount: 0,
  serviceCount: 0,
  stackCount: 0,
  hostCount: 0,
  containerCount: 0,
  imageCount: 0,
} as Summary)

async function initData() {
  const r = await systemApi.summarize();
  summary.value = r.data as Summary;
}

onMounted(() => {
  initData()
});
</script>
