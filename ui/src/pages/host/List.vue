<template>
  <x-page-header :subtitle="t('texts.records', { total: total }, total)">
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'host_new' })">
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
    <n-table size="small" :bordered="true" :single-line="false">
      <thead>
        <tr>
          <th>{{ t('fields.name') }}</th>
          <th>Endpoint</th>
          <th>{{ t('fields.status') }}</th>
          <th>Engine</th>
          <th>OS/Arch</th>
          <th>{{ t('fields.updated_at') }}</th>
          <th>{{ t('fields.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(h, index) of model" :key="h.id">
          <td>
            <x-anchor :url="{ name: 'host_detail', params: { id: h.id } }">{{ h.name }}</x-anchor>
          </td>
          <td><code>{{ h.endpoint }}</code></td>
          <td>
            <n-tag :type="statusType(h.status)" size="small">{{ h.status }}</n-tag>
          </td>
          <td>{{ h.engineVersion || '-' }}</td>
          <td>{{ h.os && h.arch ? h.os + '/' + h.arch : '-' }}</td>
          <td>
            <n-time :time="h.updatedAt" format="y-MM-dd HH:mm:ss" />
          </td>
          <td>
            <n-button size="tiny" quaternary type="info" @click="syncHost(h.id)">Sync</n-button>
            <n-button
              size="tiny"
              quaternary
              type="warning"
              @click="$router.push({ name: 'host_edit', params: { id: h.id } })"
            >{{ t('buttons.edit') }}</n-button>
            <n-popconfirm :show-icon="false" @positive-click="deleteHost(h.id, h.name, index)">
              <template #trigger>
                <n-button size="tiny" quaternary type="error">{{ t('buttons.delete') }}</n-button>
              </template>
              {{ t('prompts.delete') }}
            </n-popconfirm>
          </td>
        </tr>
      </tbody>
    </n-table>
  </n-space>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  NSpace,
  NButton,
  NTable,
  NPopconfirm,
  NIcon,
  NTime,
  NTag,
} from "naive-ui";
import { AddOutline as AddIcon } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XAnchor from "@/components/Anchor.vue";
import * as hostApi from "@/api/host";
import type { Host } from "@/api/host";
import { useStore } from "vuex";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const store = useStore();
const model = ref([] as Host[]);
const total = ref(0);

function statusType(status: string) {
  switch (status) {
    case 'connected': return 'success'
    case 'error': return 'error'
    default: return 'warning'
  }
}

async function deleteHost(id: string, name: string, index: number) {
  await hostApi.remove(id, name);
  model.value.splice(index, 1)
  total.value--
  await store.dispatch('reloadHosts')
}

async function syncHost(id: string) {
  await hostApi.sync(id);
  await fetchData();
}

async function fetchData() {
  let r = await hostApi.search();
  const data = r.data as any;
  model.value = data?.items || [];
  total.value = data?.total || 0;
}

onMounted(fetchData);
</script>
