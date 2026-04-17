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
        <n-alert
          v-if="lastDeployWarnings.length"
          type="warning"
          :title="t('messages.deploy_ignored_fields')"
          style="margin-top: 12px;"
        >
          <ul style="margin: 0; padding-left: 18px; font-size: 12px;">
            <li v-for="(w, i) of lastDeployWarnings" :key="i">{{ w }}</li>
          </ul>
        </n-alert>
        <n-alert
          v-if="lastDeployError"
          type="error"
          :title="t('stack_secret.deploy_error_title')"
          style="margin-top: 12px;"
        >
          <pre style="white-space: pre-wrap; margin: 0; font-size: 12px;">{{ lastDeployError }}</pre>
        </n-alert>
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
          :style="{ width: '100%' }"
        />

        <!-- Env vars (read-only) -->
        <div v-if="envFileContent" style="margin-top: 16px;">
          <h4 style="margin: 0 0 8px 0; font-size: 13px; opacity: 0.7;">{{ t('stack_secret.env_file_title') }}</h4>
          <pre class="env-block">{{ envFileContent }}</pre>
        </div>

        <!-- Secret bindings (read-only) -->
        <div v-if="bindings.length" style="margin-top: 16px;">
          <h4 style="margin: 0 0 8px 0; font-size: 13px; opacity: 0.7;">{{ t('stack_secret.title') }}</h4>
          <n-table size="small" :bordered="true" :single-line="false">
            <thead>
              <tr>
                <th>{{ t('objects.vault_secret') }}</th>
                <th>{{ t('objects.service') }}</th>
                <th>{{ t('stack_secret.target_type') }}</th>
                <th>{{ t('stack_secret.target') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="b of bindings" :key="b.id">
                <td>{{ b.vaultSecretId }}</td>
                <td>{{ b.service || t('stack_secret.all_services') }}</td>
                <td>
                  <n-tag size="small" :type="b.targetType === 'env' ? 'warning' : 'info'">{{ b.targetType }}</n-tag>
                </td>
                <td>
                  <code v-if="b.targetType === 'file'">{{ b.targetPath }}</code>
                  <code v-else>${{ b.envName }}</code>
                </td>
              </tr>
            </tbody>
          </n-table>
        </div>

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
  NTag, NTable, NCheckbox, useMessage,
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
import composeStackSecretApi from "@/api/compose-stack-secret";
import type { ComposeStackSecretBinding } from "@/api/compose-stack-secret";
import type { ComposeStackDetail } from "@/api/compose_stack";
import type { Container } from "@/api/container";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'
import { buildZip } from "@/utils/zip";

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
const bindings = ref<ComposeStackSecretBinding[]>([])
const envFileContent = ref('')
const lastDeployError = ref('')
const lastDeployWarnings = ref<string[]>([])

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
  envFileContent.value = s.envFile || ''
  lastDeployError.value = s.errorMessage || ''
  lastDeployWarnings.value = s.lastWarnings || []
  const r2 = await composeStackApi.findDetail(s.hostId, s.name)
  if (r2.data) {
    detail.value = r2.data
    editableContent.value = r2.data.content || ''
    await loadContainers()
    // Load secret bindings (best-effort — the tab only shows if
    // bindings exist).
    try {
      const br = await composeStackSecretApi.list(id)
      bindings.value = (br.data as any) || []
    } catch { bindings.value = [] }
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

async function downloadCompose() {
  const content = editableContent.value || detail.value.content || ''
  if (!content) return
  const name = detail.value.name || 'stack'

  // Build the .env file content
  const envContent = envFileContent.value || ''

  // Build the .secret file content — only env var names from bindings
  const secretLines = bindings.value
    .filter(b => b.targetType === 'env' && b.envName)
    .map(b => b.envName)
  const secretContent = secretLines.length
    ? '# Secret variables injected from Vault at deploy time.\n# This file lists ONLY the variable names — values live in Vault.\n' + secretLines.join('\n') + '\n'
    : ''

  // Use the JSZip-free approach: build a minimal ZIP in-memory.
  // For simplicity, download as separate files bundled via a
  // data-URL trick — or just build a proper ZIP with the tiny
  // ZIP builder below.
  const zip = buildZip([
    { name: 'docker-compose.yml', content },
    ...(envContent ? [{ name: '.env', content: envContent }] : []),
    ...(secretContent ? [{ name: '.secret', content: secretContent }] : []),
  ])
  const blob = new Blob([zip.buffer as ArrayBuffer], { type: 'application/zip' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${name}.zip`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

// buildZip + crc32 extracted to @/utils/zip.ts

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

<style scoped>
.env-block {
  white-space: pre-wrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
  /* Use a semi-transparent neutral that adapts to both light and dark
     themes without depending on a specific Naive UI CSS variable — those
     are only populated inside the n-config-provider and their fallback
     colors (e.g. #f5f5f5) render as "light on light" in dark mode. */
  background-color: rgba(128, 128, 128, 0.08);
  color: inherit;
  padding: 12px;
  border-radius: 4px;
  border: 1px solid rgba(128, 128, 128, 0.15);
  margin: 0;
}
</style>
