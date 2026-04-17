<template>
  <x-page-header :subtitle="t('texts.records', { total: model.length }, model.length)">
    <template #action>
      <n-space :size="8">
        <n-button secondary size="small" @click="refreshAll">
          <template #icon>
            <n-icon><refresh-icon /></n-icon>
          </template>
          {{ t('buttons.refresh') }}
        </n-button>
        <n-button secondary size="small" @click="$router.push({ name: 'registry_new' })">
          <template #icon>
            <n-icon><add-icon /></n-icon>
          </template>
          {{ t('buttons.new') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <n-table size="small" :bordered="true" :single-line="false">
      <thead>
        <tr>
          <th>{{ t('fields.name') }}</th>
          <th>{{ t('fields.address') }}</th>
          <th>{{ t('fields.status') }}</th>
          <th>{{ t('fields.login_name') }}</th>
          <th>{{ t('fields.updated_at') }}</th>
          <th>{{ t('fields.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(r, index) of model" :key="r.id">
          <td>
            <x-anchor :url="{ name: 'registry_detail', params: { id: r.id } }">{{ r.name }}</x-anchor>
          </td>
          <td>{{ r.url }}</td>
          <td>
            <n-tooltip v-if="pingMap[r.id]" trigger="hover">
              <template #trigger>
                <n-tag
                  size="small"
                  :type="pingMap[r.id].ok ? 'success' : 'error'"
                >{{ pingMap[r.id].ok ? t('registry.status_ok') : t('registry.status_error') }}</n-tag>
              </template>
              {{ pingMap[r.id].ok ? t('registry.status_reachable') : pingMap[r.id].error }}
            </n-tooltip>
            <n-spin v-else size="small" />
          </td>
          <td>{{ r.username }}</td>
          <td>
            <n-time :time="r.updatedAt" format="y-MM-dd HH:mm:ss" />
          </td>
          <td>
            <n-button
              size="tiny"
              quaternary
              type="warning"
              @click="$router.push({ name: 'registry_edit', params: { id: r.id } })"
            >{{ t('buttons.edit') }}</n-button>
            <n-popconfirm :show-icon="false" @positive-click="deleteRegistry(r.id, index)">
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
  NSpace, NButton, NTable, NPopconfirm, NIcon, NTime, NTag, NTooltip, NSpin,
} from "naive-ui";
import { AddOutline as AddIcon, RefreshOutline as RefreshIcon } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XAnchor from "@/components/Anchor.vue";
import registryApi from "@/api/registry";
import type { Registry } from "@/api/registry";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const model = ref<Registry[]>([])
const pingMap = ref<Record<string, { ok: boolean; error?: string }>>({})

async function deleteRegistry(id: string, index: number) {
  await registryApi.delete(id);
  model.value.splice(index, 1)
  delete pingMap.value[id]
}

async function fetchData() {
  const r = await registryApi.search();
  model.value = r.data || [];
}

async function pingAll() {
  pingMap.value = {}
  for (const r of model.value) {
    // Fire pings in parallel
    registryApi.ping(r.id).then(res => {
      pingMap.value = { ...pingMap.value, [r.id]: res.data || { ok: false, error: 'unknown' } }
    }).catch(e => {
      pingMap.value = { ...pingMap.value, [r.id]: { ok: false, error: e?.message || String(e) } }
    })
  }
}

async function refreshAll() {
  await fetchData()
  pingAll()
}

onMounted(refreshAll)
</script>
