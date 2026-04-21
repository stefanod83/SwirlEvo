<template>
  <n-space vertical :size="12">
    <!-- Discovery badge: shows the detected Traefik container + augmented
         discovery source. When no addon is detected we render a warning so
         operators know the dropdowns are seeded with generic defaults. -->
    <n-alert
      v-if="discovery"
      type="success"
      :show-icon="true"
      style="padding: 8px 12px"
    >
      <div class="muted" style="font-size: 12px">
        {{ t('stack_addon_traefik.detected') }}:
        <strong>{{ discovery.containerName || '—' }}</strong>
        <span v-if="discovery.image"> · {{ discovery.image }}</span>
        <span v-if="discovery.version"> · {{ discovery.version }}</span>
      </div>
    </n-alert>
    <n-alert v-else type="warning" :show-icon="true" style="padding: 8px 12px">
      {{ t('stack_addon_traefik.not_detected') }}
    </n-alert>

    <!-- Augmented discovery: upload a traefik.yml whose entryPoints /
         certResolvers / middlewares / networks are merged into the dropdowns
         above. The file never leaves the browser — only the extracted
         lists are POSTed to /host/addon-extract-save. -->
    <x-panel
      :title="t('stack_addon_traefik.extract_title')"
      :subtitle="t('stack_addon_traefik.extract_subtitle')"
      divider="bottom"
    >
      <template #action>
        <n-space :size="8" align="center">
          <n-tag v-if="extractMeta" size="small" type="info">
            {{ extractMeta }}
          </n-tag>
          <n-upload
            :disabled="!hostId"
            :show-file-list="false"
            accept=".yml,.yaml"
            :custom-request="handleUpload"
          >
            <n-button size="small" secondary>
              <template #icon>
                <n-icon><cloud-upload-icon /></n-icon>
              </template>
              {{ t('stack_addon_traefik.upload_btn') }}
            </n-button>
          </n-upload>
          <n-popconfirm
            v-if="hasExtract"
            :show-icon="false"
            @positive-click="clearExtract"
          >
            <template #trigger>
              <n-button size="small" quaternary type="error">
                {{ t('buttons.delete') }}
              </n-button>
            </template>
            {{ t('stack_addon_traefik.clear_confirm') }}
          </n-popconfirm>
        </n-space>
      </template>
      <div class="muted" style="font-size: 12px">
        {{ t('stack_addon_traefik.extract_hint') }}
      </div>
    </x-panel>

    <!-- Per-service form. Services derived from the compose YAML in the
         Compose tab — edits there update this list reactively. -->
    <n-alert v-if="!services.length" type="info" :show-icon="true">
      {{ t('stack_addon_traefik.no_services') }}
    </n-alert>

    <n-table v-else size="small" :bordered="true" :single-line="false">
      <thead>
        <tr>
          <th style="width: 90px">{{ t('stack_addon_traefik.enable') }}</th>
          <th>{{ t('objects.service') }}</th>
          <th>{{ t('stack_addon_traefik.rule_type') }}</th>
          <th>{{ t('stack_addon_traefik.domain') }}</th>
          <th>{{ t('stack_addon_traefik.path') }}</th>
          <th>{{ t('stack_addon_traefik.entrypoint') }}</th>
          <th style="width: 110px">{{ t('stack_addon_traefik.port') }}</th>
          <th style="width: 70px">TLS</th>
          <th>{{ t('stack_addon_traefik.certresolver') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="svc of services" :key="svc">
          <td>
            <n-switch :value="rowCfg(svc).enabled" @update:value="setRow(svc, { enabled: $event })" />
          </td>
          <td><code>{{ svc }}</code></td>
          <td>
            <n-select
              size="small"
              :value="rowCfg(svc).ruleType || 'Host'"
              :options="ruleTypeOptions"
              :disabled="!rowCfg(svc).enabled"
              style="min-width: 130px"
              @update:value="setRow(svc, { ruleType: $event })"
            />
          </td>
          <td>
            <n-input
              size="small"
              :value="rowCfg(svc).domain || ''"
              :disabled="!rowCfg(svc).enabled || rowCfg(svc).ruleType === 'PathPrefix'"
              placeholder="app.example.com"
              @update:value="setRow(svc, { domain: $event })"
            />
          </td>
          <td>
            <n-input
              size="small"
              :value="rowCfg(svc).path || ''"
              :disabled="!rowCfg(svc).enabled || rowCfg(svc).ruleType === 'Host'"
              placeholder="/api"
              @update:value="setRow(svc, { path: $event })"
            />
          </td>
          <td>
            <n-select
              size="small"
              :value="rowCfg(svc).entrypoint || ''"
              :options="entrypointOptions"
              :disabled="!rowCfg(svc).enabled"
              filterable
              tag
              clearable
              style="min-width: 140px"
              @update:value="setRow(svc, { entrypoint: $event || '' })"
            />
          </td>
          <td>
            <n-input-number
              size="small"
              :value="rowCfg(svc).port || null"
              :disabled="!rowCfg(svc).enabled"
              :min="1"
              :max="65535"
              style="width: 100%"
              @update:value="setRow(svc, { port: $event || 0 })"
            />
          </td>
          <td>
            <n-switch
              :value="!!rowCfg(svc).tls"
              :disabled="!rowCfg(svc).enabled"
              @update:value="setRow(svc, { tls: $event })"
            />
          </td>
          <td>
            <n-select
              size="small"
              :value="rowCfg(svc).certResolver || ''"
              :options="certResolverOptions"
              :disabled="!rowCfg(svc).enabled || !rowCfg(svc).tls"
              filterable
              tag
              clearable
              style="min-width: 140px"
              @update:value="setRow(svc, { certResolver: $event || '' })"
            />
          </td>
        </tr>
      </tbody>
    </n-table>

    <!-- Live preview of the labels the backend will emit. Pure cosmetic —
         the canonical generator is the Go side, re-run at Save time. -->
    <LabelPreview :labels-by-service="previewLabels" />
  </n-space>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NSpace, NAlert, NTable, NSwitch, NSelect, NInput, NInputNumber, NButton,
  NIcon, NTag, NUpload, NPopconfirm,
  useMessage,
} from 'naive-ui'
import { CloudUploadOutline as CloudUploadIcon } from '@vicons/ionicons5'
import yaml from 'js-yaml'
import { useI18n } from 'vue-i18n'
import XPanel from '@/components/Panel.vue'
import LabelPreview from './LabelPreview.vue'
import type { TraefikAddon, TraefikServiceCfg } from '@/api/compose_stack'
import type { TraefikExtract, AddonConfigExtract } from '@/api/host'
import { getAddonExtract, saveAddonExtract, clearAddonExtract } from '@/api/host'
import { buildTraefikLabels } from '@/utils/stack-addon-labels'

const props = defineProps<{
  services: string[]
  discovery: TraefikAddon | null
  mode: 'swarm' | 'standalone'
  hostId: string
  modelValue: Record<string, TraefikServiceCfg>
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: Record<string, TraefikServiceCfg>): void
}>()

const { t } = useI18n()
const message = useMessage()
const localExtract = ref<TraefikExtract | null>(null)

// Load the persisted extract on mount / hostId change so the dropdowns
// include "file-origin" entries without requiring a re-upload each session.
watch(() => props.hostId, async (hid) => {
  if (!hid) { localExtract.value = null; return }
  try {
    const r = await getAddonExtract(hid)
    localExtract.value = (r.data as AddonConfigExtract)?.traefik || null
  } catch { localExtract.value = null }
}, { immediate: true })

const hasExtract = computed(() => {
  const ex = localExtract.value
  if (!ex) return false
  return !!(ex.entryPoints?.length || ex.certResolvers?.length
    || ex.middlewares?.length || ex.networks?.length)
})

const extractMeta = computed(() => {
  const ex = localExtract.value
  if (!ex) return ''
  const parts: string[] = []
  if (ex.sourceFile) parts.push(ex.sourceFile)
  if (ex.uploadedAt) parts.push(new Date(ex.uploadedAt).toLocaleString())
  if (ex.uploadedBy) parts.push(ex.uploadedBy)
  return parts.join(' · ')
})

const ruleTypeOptions = [
  { label: 'Host(`…`)', value: 'Host' },
  { label: 'PathPrefix(`…`)', value: 'PathPrefix' },
  { label: 'Host + PathPrefix', value: 'Host+PathPrefix' },
]

// Dropdown options merge docker-discovery + file-extract with provenance
// badges so the operator knows where each entry comes from.
const entrypointOptions = computed(() => mergeOptions(
  props.discovery?.entryPoints,
  localExtract.value?.entryPoints,
))
const certResolverOptions = computed(() => mergeOptions(
  props.discovery?.certResolvers,
  localExtract.value?.certResolvers,
))

function mergeOptions(
  discovery: { name: string; origin: string }[] | undefined,
  fileNames: string[] | undefined,
): { label: string; value: string }[] {
  const out: { label: string; value: string }[] = []
  const seen = new Set<string>()
  for (const d of discovery || []) {
    if (!d.name || seen.has(d.name)) continue
    seen.add(d.name)
    out.push({ label: `${d.name} · docker`, value: d.name })
  }
  for (const n of fileNames || []) {
    if (!n || seen.has(n)) continue
    seen.add(n)
    out.push({ label: `${n} · file`, value: n })
  }
  return out
}

function rowCfg(svc: string): TraefikServiceCfg {
  return props.modelValue[svc] || {
    enabled: false, router: svc, ruleType: 'Host', tls: false, middlewares: [],
  }
}

function setRow(svc: string, patch: Partial<TraefikServiceCfg>) {
  const next = { ...props.modelValue }
  const current = next[svc] || { enabled: false, router: svc, ruleType: 'Host' }
  next[svc] = { ...current, ...patch }
  // Keep router name synced with service name until the operator edits it
  // explicitly (router is currently not exposed as a separate field).
  if (!next[svc].router) next[svc].router = svc
  emit('update:modelValue', next)
}

const previewLabels = computed(() => {
  const out: Record<string, Record<string, string>> = {}
  for (const svc of props.services) {
    const cfg = props.modelValue[svc]
    if (!cfg) continue
    const labels = buildTraefikLabels(svc, cfg)
    if (Object.keys(labels).length > 0) out[svc] = labels
  }
  return out
})

// ---- upload + persist extract -----------------------------------------

async function handleUpload({ file, onFinish, onError }: any) {
  if (!props.hostId) {
    message.error(t('stack_addon_traefik.upload_need_host'))
    onError?.()
    return
  }
  try {
    const text = await file.file.text()
    const extract = parseTraefikFile(text, file.file.name)
    await saveAddonExtract(props.hostId, { traefik: extract })
    const r = await getAddonExtract(props.hostId)
    localExtract.value = (r.data as AddonConfigExtract)?.traefik || null
    message.success(t('stack_addon_traefik.upload_ok'))
    onFinish?.()
  } catch (e: any) {
    message.error(t('stack_addon_traefik.upload_failed') + ': ' + (e?.message || String(e)))
    onError?.()
  }
}

function parseTraefikFile(text: string, filename: string): TraefikExtract {
  const doc = yaml.load(text) as any
  const entryPoints = Object.keys(doc?.entryPoints || {})
  const certResolvers = Object.keys(doc?.certificatesResolvers || {})
  const middlewares = Object.keys(doc?.http?.middlewares || {})
  const providers = doc?.providers || {}
  const networks: string[] = []
  if (providers?.docker?.network) networks.push(String(providers.docker.network))
  return {
    entryPoints, certResolvers, middlewares, networks,
    sourceFile: filename,
  }
}

async function clearExtract() {
  if (!props.hostId) return
  try {
    await clearAddonExtract(props.hostId, 'traefik')
    localExtract.value = null
    message.success(t('stack_addon_traefik.clear_ok'))
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  }
}
</script>

<style scoped>
.muted { color: var(--n-text-color-3, #999); }
</style>
