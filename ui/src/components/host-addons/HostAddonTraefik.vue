<template>
  <x-panel
    title="Traefik"
    :subtitle="t('host_addon_traefik.subtitle')"
    divider="bottom"
  >
    <template #action>
      <n-space :size="12" align="center">
        <!-- Master enable switch: when off the stack-editor Traefik tab
             disappears for every stack on this host. Config below stays
             persisted so flipping back on restores the previous state. -->
        <n-space :size="6" align="center">
          <span style="font-size: 12px">{{ t('host_addon_traefik.enabled') }}</span>
          <n-switch :value="!!local.enabled" @update:value="setEnabled" />
        </n-space>
        <!-- Upload traefik.yml: parsed client-side → only the extracted
             lists are persisted. The raw file never leaves the browser. -->
        <n-upload
          :disabled="!local.enabled"
          :show-file-list="false"
          accept=".yml,.yaml"
          :custom-request="handleUpload"
        >
          <n-button size="small" secondary :disabled="!local.enabled">
            <template #icon>
              <n-icon><cloud-upload-icon /></n-icon>
            </template>
            {{ t('host_addon_traefik.upload_btn') }}
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
          {{ t('host_addon_traefik.clear_confirm') }}
        </n-popconfirm>
      </n-space>
    </template>

    <!-- Disabled state hint: configuration form is still rendered (so
         operators can prepare it) but a banner flags that the stack
         editor tab is currently hidden. -->
    <n-alert v-if="!local.enabled" type="info" :show-icon="true" style="margin-bottom: 12px">
      {{ t('host_addon_traefik.disabled_hint') }}
    </n-alert>

    <n-alert v-if="discovery" type="success" :show-icon="true" style="margin-bottom: 12px">
      <div style="font-size: 12px">
        <strong>{{ t('host_addon_traefik.detected') }}:</strong>
        {{ discovery.containerName || '—' }}
        <span v-if="discovery.image"> · {{ discovery.image }}</span>
        <span v-if="discovery.version"> · {{ discovery.version }}</span>
      </div>
    </n-alert>

    <n-alert v-if="uploadMeta" type="info" :show-icon="false" style="margin-bottom: 12px; padding: 6px 10px">
      <span style="font-size: 12px">📄 {{ uploadMeta }}</span>
    </n-alert>

    <n-form label-placement="top" :show-feedback="false">
      <!-- Stack / container pointer ------------------------------------ -->
      <n-grid cols="2" x-gap="16" y-gap="12">
        <n-form-item-gi :label="t('host_addon_traefik.ref_stack')">
          <n-select
            v-model:value="local.stackId"
            :options="stackOptions"
            :placeholder="t('host_addon_traefik.ref_stack_placeholder')"
            filterable
            clearable
          />
        </n-form-item-gi>
        <n-form-item-gi :label="t('host_addon_traefik.container_name')">
          <n-input
            v-model:value="local.containerName"
            :placeholder="t('host_addon_traefik.container_name_placeholder')"
          />
        </n-form-item-gi>
      </n-grid>

      <!-- Defaults ----------------------------------------------------- -->
      <n-grid cols="3" x-gap="16" y-gap="12">
        <n-form-item-gi :label="t('host_addon_traefik.default_domain')">
          <n-input
            v-model:value="local.defaultDomain"
            placeholder="apps.example.com"
          />
        </n-form-item-gi>
        <n-form-item-gi :label="t('host_addon_traefik.default_entrypoint')">
          <n-select
            v-model:value="local.defaultEntrypoint"
            :options="entrypointOptions"
            filterable
            tag
            clearable
            :placeholder="t('host_addon_traefik.default_entrypoint_placeholder')"
          />
        </n-form-item-gi>
        <n-form-item-gi :label="t('host_addon_traefik.default_certresolver')">
          <n-select
            v-model:value="local.defaultCertResolver"
            :options="certResolverOptions"
            filterable
            tag
            clearable
            :placeholder="t('host_addon_traefik.default_certresolver_placeholder')"
          />
        </n-form-item-gi>
      </n-grid>

      <!-- Extracted lists: read-only display, but editable via tag input
           so the operator can prune / add manually. --------------------- -->
      <n-grid cols="2" x-gap="16" y-gap="12">
        <n-form-item-gi :label="t('host_addon_traefik.entrypoints')">
          <n-dynamic-tags v-model:value="entryPointsList" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('host_addon_traefik.certresolvers')">
          <n-dynamic-tags v-model:value="certResolversList" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('host_addon_traefik.middlewares')">
          <n-dynamic-tags v-model:value="middlewaresList" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('host_addon_traefik.networks')">
          <n-dynamic-tags v-model:value="networksList" />
        </n-form-item-gi>
      </n-grid>

      <!-- Overrides — free-form key/value pairs for anything not captured
           by the structured fields above. ------------------------------- -->
      <n-form-item :label="t('host_addon_traefik.overrides')">
        <div style="width: 100%">
          <n-table size="small" :bordered="true" :single-line="false">
            <thead>
              <tr>
                <th style="width: 40%">{{ t('fields.key') }}</th>
                <th>{{ t('fields.value') }}</th>
                <th style="width: 60px"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, idx) of overridesRows" :key="idx">
                <td><n-input size="small" v-model:value="row.k" /></td>
                <td><n-input size="small" v-model:value="row.v" /></td>
                <td>
                  <n-button size="tiny" quaternary type="error" @click="removeOverride(idx)">
                    {{ t('buttons.delete') }}
                  </n-button>
                </td>
              </tr>
              <tr v-if="!overridesRows.length">
                <td colspan="3" style="text-align: center; padding: 8px;" class="muted">
                  {{ t('host_addon_traefik.overrides_empty') }}
                </td>
              </tr>
            </tbody>
          </n-table>
          <n-button size="small" quaternary @click="addOverride" style="margin-top: 8px">
            <template #icon>
              <n-icon><add-icon /></n-icon>
            </template>
            {{ t('host_addon_traefik.add_override') }}
          </n-button>
        </div>
      </n-form-item>

      <n-space>
        <n-button type="primary" :loading="saving" @click="save">
          <template #icon>
            <n-icon><save-icon /></n-icon>
          </template>
          {{ t('buttons.save') }}
        </n-button>
      </n-space>
    </n-form>
  </x-panel>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  NSpace, NButton, NIcon, NAlert, NForm, NFormItem, NFormItemGi, NGrid, NInput,
  NSelect, NSwitch, NDynamicTags, NTable, NUpload, NPopconfirm,
  useMessage,
} from 'naive-ui'
import {
  CloudUploadOutline as CloudUploadIcon,
  AddOutline as AddIcon,
  SaveOutline as SaveIcon,
} from '@vicons/ionicons5'
import yaml from 'js-yaml'
import { useI18n } from 'vue-i18n'
import XPanel from '@/components/Panel.vue'
import {
  getAddonExtract, saveAddonExtract, clearAddonExtract,
  type AddonConfigExtract, type TraefikExtract,
} from '@/api/host'
import composeStackApi from '@/api/compose_stack'
import type { TraefikAddon, DiscoveryValue } from '@/api/compose_stack'

const props = defineProps<{ hostId: string }>()
const { t } = useI18n()
const message = useMessage()

const saving = ref(false)
const discovery = ref<TraefikAddon | null>(null)
const local = reactive<TraefikExtract>({
  enabled: false,
  entryPoints: [], certResolvers: [], middlewares: [], networks: [],
  stackId: '', containerName: '',
  defaultDomain: '', defaultEntrypoint: '', defaultCertResolver: '',
  overrides: {},
})
// Non-reactive wrappers for n-dynamic-tags (binds to arrays).
const entryPointsList = ref<string[]>([])
const certResolversList = ref<string[]>([])
const middlewaresList = ref<string[]>([])
const networksList = ref<string[]>([])
// Overrides editor: convert map<->rows lazily so the table can stay
// reactive even when keys rename.
const overridesRows = ref<{ k: string; v: string }[]>([])

const stackOptions = ref<{ label: string; value: string }[]>([])

async function load() {
  if (!props.hostId) return
  // Extract + discovery + stacks (for reference dropdown) in parallel.
  const [extractRes, discoveryRes, stacksRes] = await Promise.allSettled([
    getAddonExtract(props.hostId),
    composeStackApi.hostAddons(props.hostId),
    composeStackApi.search({ hostId: props.hostId, pageIndex: 1, pageSize: 1000 }),
  ])

  if (extractRes.status === 'fulfilled') {
    const t0 = (extractRes.value.data as AddonConfigExtract)?.traefik
    if (t0) applyLocal(t0)
  }
  if (discoveryRes.status === 'fulfilled') {
    discovery.value = (discoveryRes.value.data as any)?.traefik || null
  }
  if (stacksRes.status === 'fulfilled') {
    // compose-stack/search returns both managed (persisted, have id) and
    // external (CLI-discovered, id=="") stacks. Only managed ones can be
    // referenced here — external stacks have no stable identifier to
    // persist on Host.AddonConfigExtract. Filter + sort alphabetically.
    const items = ((stacksRes.value.data as any)?.items || []) as { id: string; name: string; managed?: boolean }[]
    stackOptions.value = items
      .filter((s) => !!s.id && s.managed !== false)
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((s) => ({ label: s.name, value: s.id }))
  }
}

function applyLocal(t: TraefikExtract) {
  local.enabled = !!t.enabled
  local.entryPoints = t.entryPoints || []
  local.certResolvers = t.certResolvers || []
  local.middlewares = t.middlewares || []
  local.networks = t.networks || []
  local.stackId = t.stackId || ''
  local.containerName = t.containerName || ''
  local.defaultDomain = t.defaultDomain || ''
  local.defaultEntrypoint = t.defaultEntrypoint || ''
  local.defaultCertResolver = t.defaultCertResolver || ''
  local.overrides = { ...(t.overrides || {}) }
  local.sourceFile = t.sourceFile
  local.uploadedAt = t.uploadedAt
  local.uploadedBy = t.uploadedBy
  entryPointsList.value = [...local.entryPoints!]
  certResolversList.value = [...local.certResolvers!]
  middlewaresList.value = [...local.middlewares!]
  networksList.value = [...local.networks!]
  overridesRows.value = Object.keys(local.overrides!).map((k) => ({ k, v: local.overrides![k] }))
}

const hasExtract = computed(() =>
  !!(local.entryPoints?.length || local.certResolvers?.length
    || local.middlewares?.length || local.networks?.length
    || local.stackId || local.containerName
    || local.defaultDomain || local.defaultEntrypoint || local.defaultCertResolver
    || Object.keys(local.overrides || {}).length
    || local.sourceFile)
)

const uploadMeta = computed(() => {
  if (!local.sourceFile) return ''
  const parts: string[] = [local.sourceFile]
  if (local.uploadedAt) parts.push(new Date(local.uploadedAt).toLocaleString())
  if (local.uploadedBy) parts.push(local.uploadedBy)
  return parts.join(' · ')
})

// Discovery dropdowns for the default-value selects: docker-detected names
// first, then anything added manually via the tag lists above.
const entrypointOptions = computed(() => discoveryOpts(
  discovery.value?.entryPoints, entryPointsList.value,
))
const certResolverOptions = computed(() => discoveryOpts(
  discovery.value?.certResolvers, certResolversList.value,
))

function discoveryOpts(det: DiscoveryValue[] | undefined, list: string[]): { label: string; value: string }[] {
  const out: { label: string; value: string }[] = []
  const seen = new Set<string>()
  for (const d of det || []) {
    if (!d.name || seen.has(d.name)) continue
    seen.add(d.name)
    out.push({ label: `${d.name} · docker`, value: d.name })
  }
  for (const n of list) {
    if (!n || seen.has(n)) continue
    seen.add(n)
    out.push({ label: n, value: n })
  }
  return out
}

function addOverride() {
  overridesRows.value.push({ k: '', v: '' })
}

function removeOverride(idx: number) {
  overridesRows.value.splice(idx, 1)
}

// setEnabled flips the master toggle and saves immediately — operators
// expect the stack-editor tab to appear/disappear without an extra Save
// click. Other field edits still require the explicit Save button so a
// half-typed override doesn't get persisted on every keystroke.
async function setEnabled(v: boolean) {
  local.enabled = v
  await save()
}

async function handleUpload({ file, onFinish, onError }: any) {
  try {
    const text = await file.file.text()
    const parsed = parseTraefikFile(text)
    // Merge with current lists instead of replacing — the operator may
    // have added entries manually that the file doesn't cover.
    entryPointsList.value = unionOrdered(entryPointsList.value, parsed.entryPoints)
    certResolversList.value = unionOrdered(certResolversList.value, parsed.certResolvers)
    middlewaresList.value = unionOrdered(middlewaresList.value, parsed.middlewares)
    networksList.value = unionOrdered(networksList.value, parsed.networks)
    local.sourceFile = file.file.name
    await save()
    onFinish?.()
  } catch (e: any) {
    message.error(t('host_addon_traefik.upload_failed') + ': ' + (e?.message || String(e)))
    onError?.()
  }
}

function unionOrdered(a: string[], b: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const v of [...a, ...b]) {
    if (!v || seen.has(v)) continue
    seen.add(v)
    out.push(v)
  }
  return out
}

function parseTraefikFile(text: string) {
  const doc = yaml.load(text) as any
  const entryPoints = Object.keys(doc?.entryPoints || {})
  const certResolvers = Object.keys(doc?.certificatesResolvers || {})
  const middlewares = Object.keys(doc?.http?.middlewares || {})
  const networks: string[] = []
  if (doc?.providers?.docker?.network) networks.push(String(doc.providers.docker.network))
  return { entryPoints, certResolvers, middlewares, networks }
}

async function clearExtract() {
  try {
    await clearAddonExtract(props.hostId, 'traefik')
    // Reset local state to defaults.
    applyLocal({})
    message.success(t('host_addon_traefik.clear_ok'))
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  }
}

async function save() {
  saving.value = true
  try {
    // Flatten current editor state into a TraefikExtract payload.
    const overrides: Record<string, string> = {}
    for (const r of overridesRows.value) {
      if (r.k.trim()) overrides[r.k.trim()] = r.v
    }
    const payload: TraefikExtract = {
      enabled: !!local.enabled,
      entryPoints: [...entryPointsList.value],
      certResolvers: [...certResolversList.value],
      middlewares: [...middlewaresList.value],
      networks: [...networksList.value],
      stackId: local.stackId || '',
      containerName: local.containerName || '',
      defaultDomain: local.defaultDomain || '',
      defaultEntrypoint: local.defaultEntrypoint || '',
      defaultCertResolver: local.defaultCertResolver || '',
      overrides,
      sourceFile: local.sourceFile || '',
    }
    await saveAddonExtract(props.hostId, { traefik: payload })
    message.success(t('host_addon_traefik.save_ok'))
    await load()
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    saving.value = false
  }
}

watch(() => props.hostId, () => { if (props.hostId) load() })
onMounted(() => { if (props.hostId) load() })
</script>

<style scoped>
.muted { color: var(--n-text-color-3, #999); }
</style>
