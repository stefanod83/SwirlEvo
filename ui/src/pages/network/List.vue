<template>
  <x-page-header :subtitle="t('texts.records', { total: model.length }, model.length)">
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'network_new' })">
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
    <x-empty-host-prompt v-if="showEmpty" :resource="t('objects.network', 2)" />
    <n-table v-else size="small" :bordered="true" :single-line="false">
      <thead>
        <tr>
          <th>{{ t('fields.name') }}</th>
          <th>{{ t('fields.id') }}</th>
          <th>{{ t('fields.scope') }}</th>
          <th>{{ t('fields.driver') }}</th>
          <th>{{ t('fields.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(r, index) of model" :key="r.name">
          <td>
            <n-space :size="6" align="center" inline>
              <x-anchor :url="{ name: 'network_detail', params: { name: r.name } }">{{ r.name }}</x-anchor>
              <n-tag v-if="r.unused" round size="small" type="warning">{{ t('fields.unused') }}</n-tag>
            </n-space>
          </td>
          <td>{{ r.id }}</td>
          <td>
            <n-tag
              round
              size="small"
              :type="r.scope === 'swarm' ? 'success' : 'default'"
            >{{ r.scope }}</n-tag>
          </td>
          <td>
            <n-tag
              round
              size="small"
              :type="r.driver === 'overlay' ? 'success' : 'default'"
            >{{ r.driver }}</n-tag>
          </td>
          <td>
            <n-popconfirm :show-icon="false" @positive-click="deleteNetwork(r.id, r.name, index)">
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
  NTag,
  NIcon,
} from "naive-ui";
import { AddOutline as AddIcon } from "@vicons/ionicons5";
import XAnchor from "@/components/Anchor.vue";
import XPageHeader from "@/components/PageHeader.vue";
import networkApi from "@/api/network";
import type { Network } from "@/api/network";
import { computed, watch } from "vue";
import { useStore } from "vuex";
import XEmptyHostPrompt from "@/components/EmptyHostPrompt.vue";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const store = useStore()
const isStandalone = computed(() => store.state.mode === 'standalone')
const selectedHostId = computed(() => store.state.selectedHostId as string | null)
const showEmpty = computed(() => isStandalone.value && !selectedHostId.value)
const model = ref([] as Network[]);

async function deleteNetwork(id: string, name: string, index: number) {
  await networkApi.delete(id, name);
  model.value.splice(index, 1)
}

async function fetchData() {
  let r = await networkApi.search();
  model.value = r.data || [];
}

watch(selectedHostId, (v) => { if (v || !isStandalone.value) fetchData() })
onMounted(() => { if (!showEmpty.value) fetchData() })
</script>