<template>
  <x-page-header :subtitle="t('texts.records', { total: model.length }, model.length)">
    <template #action>
      <n-button secondary size="small" :disabled="!selectedHostId" @click="$router.push({ name: 'std_network_new' })">
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
          <td>{{ r.name }}</td>
          <td>{{ r.id }}</td>
          <td>
            <n-tag round size="small" :type="r.scope === 'swarm' ? 'success' : 'default'">{{ r.scope }}</n-tag>
          </td>
          <td>
            <n-tag round size="small" :type="r.driver === 'overlay' ? 'success' : 'default'">{{ r.driver }}</n-tag>
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
import { computed, onMounted, ref, watch } from "vue";
import {
  NSpace,
  NButton,
  NTable,
  NPopconfirm,
  NTag,
  NIcon,
} from "naive-ui";
import { AddOutline as AddIcon } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XEmptyHostPrompt from "@/components/EmptyHostPrompt.vue";
import networkApi from "@/api/network";
import type { Network } from "@/api/network";
import { useStore } from "vuex";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const store = useStore()
const selectedHostId = computed(() => store.state.selectedHostId as string | null)
const showEmpty = computed(() => !selectedHostId.value)
const model = ref([] as Network[]);

async function deleteNetwork(id: string, name: string, index: number) {
  await networkApi.delete(id, name, selectedHostId.value || '');
  model.value.splice(index, 1)
}

async function fetchData() {
  if (!selectedHostId.value) { model.value = []; return }
  let r = await networkApi.search(selectedHostId.value);
  model.value = r.data || [];
}

watch(selectedHostId, fetchData)
onMounted(fetchData);
</script>
