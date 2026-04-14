<template>
  <x-page-header :subtitle="detail.name">
    <template #action>
      <n-space>
        <n-button secondary size="small" @click="$router.push({ name: 'std_stack_list' })">
          <template #icon>
            <n-icon><arrow-back-icon /></n-icon>
          </template>
          {{ t('buttons.return') }}
        </n-button>
        <n-button secondary size="small" :disabled="!detail.content" @click="downloadCompose">
          <template #icon>
            <n-icon><download-outline /></n-icon>
          </template>
          {{ t('buttons.download') || 'Download' }}
        </n-button>
        <template v-if="detail.managed && detail.id">
          <n-button secondary size="small" type="primary" @click="$router.push({ name: 'std_stack_edit', params: { id: detail.id } })">
            <template #icon>
              <n-icon><create-outline /></n-icon>
            </template>
            {{ t('buttons.edit') }}
          </n-button>
        </template>
      </n-space>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12" v-if="detail.name">
    <n-alert v-if="!detail.managed" type="warning" :show-icon="true">
      {{ t('texts.external_stack_warning') || 'This stack was created outside Swirl. Review the reconstructed compose file below, then click Import to have Swirl manage it.' }}
    </n-alert>

    <n-tabs type="line" size="small">
      <n-tab-pane name="overview" :tab="t('fields.overview') || 'Overview'">
        <n-descriptions label-placement="left" bordered :column="2" size="small">
          <n-descriptions-item :label="t('objects.host')">{{ detail.hostName || detail.hostId }}</n-descriptions-item>
          <n-descriptions-item :label="t('fields.name')">{{ detail.name }}</n-descriptions-item>
          <n-descriptions-item :label="t('fields.status')">
            <n-tag size="small" :type="statusType(detail.status)">{{ detail.status }}</n-tag>
          </n-descriptions-item>
          <n-descriptions-item :label="t('fields.managed') || 'Managed'">
            <n-tag size="small" :type="detail.managed ? 'success' : 'default'">
              {{ detail.managed ? (t('enums.yes') || 'Yes') : (t('enums.no') || 'No') }}
            </n-tag>
          </n-descriptions-item>
          <n-descriptions-item :label="t('objects.service', 2)">{{ detail.services.join(', ') || '-' }}</n-descriptions-item>
          <n-descriptions-item :label="t('objects.network', 2)">{{ detail.networks.join(', ') || '-' }}</n-descriptions-item>
          <n-descriptions-item :label="t('objects.volume', 2)">{{ detail.volumes.join(', ') || '-' }}</n-descriptions-item>
          <n-descriptions-item v-if="detail.updatedAt" :label="t('fields.updated_at')">{{ detail.updatedAt }}</n-descriptions-item>
        </n-descriptions>
      </n-tab-pane>

      <n-tab-pane name="containers" :tab="t('objects.container', 2)">
        <x-container-table
          :node="detail.hostId"
          :data="containers as any"
          :loading="containersLoading"
          :show-stack-column="false"
          @refresh="loadContainers"
        />
      </n-tab-pane>

      <n-tab-pane name="compose" :tab="t('fields.compose_yaml') || 'Compose (YAML)'">
        <n-alert v-if="detail.reconstructed" type="info" :show-icon="true" style="margin-bottom:12px">
          {{ t('texts.reconstructed_compose_notice') || 'YAML reconstructed from running containers. Some fields cannot be derived at runtime — review before importing.' }}
        </n-alert>
        <x-code-mirror
          v-model="editableContent"
          :readonly="detail.managed"
          height="70vh"
          :style="{ width: '100%', border: '1px solid #ddd' }"
        />
        <n-space v-if="!detail.managed" style="margin-top:12px">
          <n-checkbox v-model:checked="pullImages">{{ t('fields.pull_images') || 'Pull images' }}</n-checkbox>
          <n-button type="primary" :loading="submitting" @click="doImport(true)">
            <template #icon>
              <n-icon><rocket-outline /></n-icon>
            </template>
            {{ t('buttons.import_redeploy') || 'Import & Redeploy' }}
          </n-button>
          <n-button secondary :loading="submitting" @click="doImport(false)">
            <template #icon>
              <n-icon><download-outline /></n-icon>
            </template>
            {{ t('buttons.import') || 'Import' }}
          </n-button>
        </n-space>
      </n-tab-pane>
    </n-tabs>
  </n-space>
</template>

<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import {
  NSpace, NButton, NIcon, NDescriptions, NDescriptionsItem, NAlert, NTabs, NTabPane,
  NTag, NCheckbox, useMessage,
} from "naive-ui";
import {
  ArrowBackCircleOutline as ArrowBackIcon,
  CreateOutline, RocketOutline, DownloadOutline,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XCodeMirror from "@/components/CodeMirror.vue";
import XContainerTable from "@/components/ContainerTable.vue";
import composeStackApi from "@/api/compose_stack";
import containerApi from "@/api/container";
import type { ComposeStackDetail } from "@/api/compose_stack";
import type { Container } from "@/api/container";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const message = useMessage()
const detail = ref({ services: [], networks: [], volumes: [], containers: [] } as any as ComposeStackDetail)
const editableContent = ref('')
const pullImages = ref(false)
const submitting = ref(false)
const containers = ref([] as Container[])
const containersLoading = ref(false)

async function loadContainers() {
  if (!detail.value.hostId || !detail.value.name) return
  containersLoading.value = true
  try {
    const r = await containerApi.search({
      node: detail.value.hostId,
      project: detail.value.name,
      pageIndex: 1,
      pageSize: 100,
    } as any)
    containers.value = (r.data?.items || []) as Container[]
  } catch {
    containers.value = []
  } finally {
    containersLoading.value = false
  }
}

function statusType(s: string): any {
  if (s === 'active') return 'success'
  if (s === 'partial') return 'warning'
  if (s === 'error') return 'error'
  return 'default'
}

async function loadByIdAndResolve(id: string) {
  const r = await composeStackApi.find(id)
  if (!r.data) return
  const s = r.data
  const r2 = await composeStackApi.findDetail(s.hostId, s.name)
  if (r2.data) {
    detail.value = r2.data
    editableContent.value = r2.data.content || ''
    await loadContainers()
  }
}

async function loadByHostAndName(hostId: string, name: string) {
  const r = await composeStackApi.findDetail(hostId, name)
  if (r.data) {
    detail.value = r.data
    editableContent.value = r.data.content || ''
    await loadContainers()
  }
}

function downloadCompose() {
  const content = editableContent.value || detail.value.content || ''
  if (!content) return
  const blob = new Blob([content], { type: 'application/x-yaml' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${detail.value.name || 'stack'}.yml`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

async function doImport(redeploy: boolean) {
  if (!detail.value) return
  submitting.value = true
  try {
    const r = await composeStackApi.import_({
      hostId: detail.value.hostId,
      name: detail.value.name,
      content: editableContent.value,
    }, redeploy, pullImages.value)
    message.success(t('buttons.import') || 'Imported')
    if (r.data?.id) {
      router.push({ name: 'std_stack_detail', params: { id: r.data.id } })
    }
  } catch (e: any) {
    message.error(e?.message || String(e))
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  if (route.name === 'std_stack_external_detail') {
    await loadByHostAndName(route.params.hostId as string, route.params.name as string)
  } else if (route.params.id) {
    await loadByIdAndResolve(route.params.id as string)
  }
})
</script>
