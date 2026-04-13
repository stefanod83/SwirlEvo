<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'host_edit', params: { id: model.id } })">
        {{ t('buttons.edit') }}
      </n-button>
      <n-button secondary size="small" @click="syncHost">Sync</n-button>
    </template>
  </x-page-header>
  <div class="page-body" v-if="model.id">
    <n-space vertical :size="16">
      <x-description label-placement="left" label-align="right" :column="2" :label-width="120">
        <x-description-item :label="t('fields.name')">{{ model.name }}</x-description-item>
        <x-description-item label="Endpoint"><code>{{ model.endpoint }}</code></x-description-item>
        <x-description-item :label="t('fields.status')">
          <n-tag :type="statusType(model.status || '')" size="small">{{ model.status }}</n-tag>
        </x-description-item>
        <x-description-item label="Auth">{{ model.authMethod }}</x-description-item>
        <x-description-item label="Engine">{{ model.engineVersion || '-' }}</x-description-item>
        <x-description-item label="OS/Arch">{{ model.os && model.arch ? model.os + '/' + model.arch : '-' }}</x-description-item>
        <x-description-item label="CPUs">{{ model.cpus || '-' }}</x-description-item>
        <x-description-item label="Memory">{{ model.memory ? formatBytes(model.memory) : '-' }}</x-description-item>
        <x-description-item v-if="model.error" label="Error">
          <n-tag type="error" size="small">{{ model.error }}</n-tag>
        </x-description-item>
        <x-description-item :label="t('fields.created_at')">
          <n-time :time="model.createdAt" format="y-MM-dd HH:mm:ss" />
        </x-description-item>
        <x-description-item :label="t('fields.updated_at')">
          <n-time :time="model.updatedAt" format="y-MM-dd HH:mm:ss" />
        </x-description-item>
      </x-description>
    </n-space>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import {
  NSpace,
  NButton,
  NTag,
  NTime,
} from "naive-ui";
import XPageHeader from "@/components/PageHeader.vue";
import XDescription from "@/components/description/Description.vue";
import XDescriptionItem from "@/components/description/DescriptionItem.vue";
import * as hostApi from "@/api/host";
import type { Host } from "@/api/host";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute();
const model = ref({} as Partial<Host> & { id?: string });

function statusType(status: string) {
  switch (status) {
    case 'connected': return 'success'
    case 'error': return 'error'
    default: return 'warning'
  }
}

function formatBytes(bytes: number) {
  const gb = bytes / (1024 * 1024 * 1024)
  return gb.toFixed(1) + ' GB'
}

async function syncHost() {
  await hostApi.sync(model.value.id!);
  await fetchData();
}

async function fetchData() {
  const id = route.params.id as string;
  const r = await hostApi.find(id);
  if (r.data) model.value = r.data as any;
}

onMounted(fetchData);
</script>
