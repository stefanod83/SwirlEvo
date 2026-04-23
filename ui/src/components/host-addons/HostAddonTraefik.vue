<template>
  <x-panel
    title="Traefik"
    :subtitle="t('host_addon_traefik.subtitle')"
    divider="bottom"
    :collapsed="effectiveCollapsed"
  >
    <template #action>
      <n-space :size="12" align="center">
        <!-- Collapse toggle — only rendered when the parent controls
             the panel state (opt-in via the `collapsed` prop). Keeps
             the component backward-compatible with callers that embed
             it as a single always-expanded addon. -->
        <n-button
          v-if="isControlled"
          secondary
          strong
          size="small"
          style="min-width: 75px"
          @click="() => emit('toggle')"
        >{{ effectiveCollapsed ? t('buttons.expand') : t('buttons.collapse') }}</n-button>
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
          multiple
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
          :show-icon="false"
          @positive-click="resetAll"
        >
          <template #trigger>
            <n-button size="small" quaternary type="warning">
              {{ t('host_addon_traefik.reset_btn') }}
            </n-button>
          </template>
          {{ t('host_addon_traefik.reset_confirm') }}
        </n-popconfirm>
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

    <n-space vertical :size="16">

      <!-- (a) Detection / Metadata ================================== -->
      <section
        v-if="!local.enabled || discovery || uploadMeta"
        class="addon-section addon-section--meta"
      >
        <n-alert
          v-if="!local.enabled"
          type="info"
          :show-icon="true"
          class="addon-alert"
        >
          {{ t('host_addon_traefik.disabled_hint') }}
        </n-alert>

        <n-alert
          v-if="discovery"
          type="success"
          :show-icon="true"
          class="addon-alert"
        >
          <template #icon>
            <n-icon><cube-outline-icon /></n-icon>
          </template>
          <div class="addon-detection">
            <strong>{{ t('host_addon_traefik.detected') }}:</strong>
            <span class="addon-detection__name">{{ discovery.containerName || '—' }}</span>
            <span v-if="discovery.image" class="addon-detection__meta">· {{ discovery.image }}</span>
            <span v-if="discovery.version" class="addon-detection__meta">· {{ discovery.version }}</span>
            <span v-if="uploadMeta" class="addon-detection__upload">· {{ uploadMeta }}</span>
          </div>
        </n-alert>

        <n-alert
          v-else-if="uploadMeta"
          type="info"
          :show-icon="true"
          class="addon-alert"
        >
          <template #icon>
            <n-icon><document-icon /></n-icon>
          </template>
          <span class="addon-upload-meta">{{ uploadMeta }}</span>
        </n-alert>
      </section>

      <n-form label-placement="top" :show-feedback="false">

        <!-- (b) Pointer ============================================ -->
        <section class="addon-section">
          <h4 class="addon-section-title">{{ t('host_addon_generic.pointer') }}</h4>
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
        </section>

        <n-divider class="addon-divider" />

        <!--
          (c) Defaults ============================================
          Uses n-grid with cols=1 so every <n-form-item-gi> renders
          its label consistently with the other sections. A plain
          <n-form-item> outside a grid was dropping the label in
          some naive-ui releases; the -gi variant inside a grid is
          the canonical pattern here.
        -->
        <!--
          (c) Defaults ============================================
          Plain <label> + widget pairs in a CSS grid. Dropped the
          n-form / n-form-item dependency here — it was silently
          hiding labels when the field was a plain n-form-item
          outside a grid. Labels are now explicit HTML so there is
          nothing to go wrong theme-side.

          Default middleware is a multi-select with tag support:
          picks from the Traefik inventory (docker + host-extract
          lists) OR accepts custom entries typed by the operator.
          Value is serialised as a comma-separated string in
          local.defaultMiddleware so the backend keeps the existing
          shape (it's a single `traefik.http.routers.<r>.middlewares`
          label at save time).
        -->
        <section class="addon-section">
          <h4 class="addon-section-title">{{ t('host_addon_generic.defaults') }}</h4>
          <div class="defaults-grid">
            <div class="field">
              <label class="field-label">{{ t('host_addon_traefik.default_domain') }}</label>
              <n-input
                v-model:value="local.defaultDomain"
                placeholder="apps.example.com"
              />
            </div>
            <div class="field">
              <label class="field-label">{{ t('host_addon_traefik.default_entrypoint') }}</label>
              <n-select
                v-model:value="local.defaultEntrypoint"
                :options="entrypointOptions"
                filterable
                tag
                clearable
                :placeholder="t('host_addon_traefik.default_entrypoint_placeholder')"
              />
            </div>
            <div class="field">
              <label class="field-label">{{ t('host_addon_traefik.default_certresolver') }}</label>
              <n-select
                v-model:value="local.defaultCertResolver"
                :options="certResolverOptions"
                filterable
                tag
                clearable
                :placeholder="t('host_addon_traefik.default_certresolver_placeholder')"
              />
            </div>
            <div class="field">
              <label class="field-label">{{ t('host_addon_traefik.default_middleware') }}</label>
              <n-select
                :value="splitCsv(local.defaultMiddleware)"
                :options="middlewareOptions"
                multiple
                filterable
                tag
                clearable
                :placeholder="t('host_addon_traefik.default_middleware_placeholder')"
                @update:value="(v: string[]) => (local.defaultMiddleware = joinCsv(v))"
              />
            </div>
          </div>
        </section>

        <n-divider class="addon-divider" />

        <!-- (d) Inventory ========================================== -->
        <section class="addon-section">
          <h4 class="addon-section-title">{{ t('host_addon_generic.inventory') }}</h4>
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
        </section>

        <n-divider class="addon-divider" />

        <!-- (e) Custom overrides =================================== -->
        <section class="addon-section">
          <div class="addon-section-header">
            <h4 class="addon-section-title">{{ t('host_addon_traefik.overrides') }}</h4>
            <span class="addon-section-hint muted">{{ t('host_addon_traefik.overrides_desc') }}</span>
          </div>
          <n-table size="small" :bordered="true" :single-line="false" class="addon-overrides-table">
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
                <td colspan="3" class="addon-overrides-empty muted">
                  {{ t('host_addon_traefik.overrides_empty') }}
                </td>
              </tr>
            </tbody>
          </n-table>
          <div class="addon-overrides-footer">
            <n-button size="small" quaternary @click="addOverride">
              <template #icon>
                <n-icon><add-icon /></n-icon>
              </template>
              {{ t('host_addon_traefik.add_override') }}
            </n-button>
          </div>
        </section>

        <n-divider class="addon-divider" />

        <!-- Save bar ============================================== -->
        <n-space justify="end" class="addon-actions">
          <n-button type="primary" :loading="saving" @click="save">
            <template #icon>
              <n-icon><save-icon /></n-icon>
            </template>
            {{ t('buttons.save') }}
          </n-button>
        </n-space>
      </n-form>
    </n-space>
  </x-panel>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  NSpace, NButton, NIcon, NAlert, NForm, NFormItemGi, NGrid, NInput,
  NSelect, NSwitch, NDynamicTags, NTable, NUpload, NPopconfirm, NDivider,
  useMessage,
} from 'naive-ui'
import {
  CloudUploadOutline as CloudUploadIcon,
  AddOutline as AddIcon,
  SaveOutline as SaveIcon,
  CubeOutline as CubeOutlineIcon,
  DocumentOutline as DocumentIcon,
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

const props = defineProps<{
  hostId: string
  // When set, the parent controls the expanded/collapsed state of the
  // inner panel (Settings-style). Leaving it undefined keeps the
  // legacy behavior: panel is always expanded and no toggle button
  // appears in the action slot.
  collapsed?: boolean
}>()
const emit = defineEmits<{
  (e: 'toggle'): void
}>()
const { t } = useI18n()
const message = useMessage()

// Controlled = parent passes `collapsed` (true OR false). Uncontrolled
// = prop is undefined → always expanded, no toggle button.
const isControlled = computed(() => props.collapsed !== undefined)
const effectiveCollapsed = computed(() => isControlled.value ? !!props.collapsed : false)

const saving = ref(false)
const discovery = ref<TraefikAddon | null>(null)
const local = reactive<TraefikExtract>({
  enabled: false,
  entryPoints: [], certResolvers: [], middlewares: [], networks: [],
  stackId: '', containerName: '',
  defaultDomain: '', defaultEntrypoint: '', defaultCertResolver: '', defaultMiddleware: '',
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
    // compose-stack/search returns both managed (persisted, with id) and
    // unmanaged (CLI-discovered, id==""). Operators often run Traefik
    // outside Swirl's management (e.g. via docker compose CLI); they
    // still want to point the reference to it. For unmanaged we use the
    // project name as the identifier, with an "unmanaged" tag in the
    // label so users know what they're selecting.
    const items = ((stacksRes.value.data as any)?.items || []) as { id: string; name: string; managed?: boolean }[]
    const managed = items
      .filter((s) => !!s.id && s.managed !== false)
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((s) => ({ label: s.name, value: s.id }))
    const unmanaged = items
      .filter((s) => !s.id || s.managed === false)
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((s) => ({
        label: `${s.name} · ${t('host_addon_traefik.ref_unmanaged')}`,
        value: `external:${s.name}`,
      }))
    stackOptions.value = [...managed, ...unmanaged]
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
  local.defaultMiddleware = t.defaultMiddleware || ''
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
// Middleware options: same "docker · <name>" / "<name>" badging as the
// stack-editor Traefik tab. Operator can tag a custom middleware
// (provider-qualified too, e.g. auth@file) that isn't in the
// discovery/inventory lists — the tag prop on n-select accepts it.
const middlewareOptions = computed(() => discoveryOpts(
  discovery.value?.middlewares, middlewaresList.value,
))

// splitCsv / joinCsv bridge between the comma-separated string stored
// in local.defaultMiddleware (backend contract — it becomes the value
// of `traefik.http.routers.<r>.middlewares` on save) and the array
// model n-select multiple expects.
function splitCsv(v: string | undefined): string[] {
  if (!v) return []
  return v.split(',').map((s) => s.trim()).filter(Boolean)
}
function joinCsv(arr: unknown): string {
  if (!Array.isArray(arr)) return ''
  return (arr as string[]).filter(Boolean).join(',')
}

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

// resetAll wipes every form field in-memory (keeping `enabled` so the
// operator can preview the blank state without flipping the master
// toggle) — the next Save persists the blank payload. Differs from
// clearExtract in that it doesn't issue a server-side delete, so the
// operator can back out by refreshing before saving.
function resetAll() {
  applyLocal({ enabled: local.enabled })
  message.info(t('host_addon_traefik.reset_done'))
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
      defaultMiddleware: local.defaultMiddleware || '',
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

.addon-section {
  display: block;
}

.addon-section--meta {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.addon-section-title {
  font-size: 13px;
  font-weight: 600;
  margin: 0 0 10px 0;
  color: var(--n-text-color-2, #555);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.addon-section-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.addon-section-header .addon-section-title {
  margin-bottom: 0;
}

.addon-section-hint {
  font-size: 12px;
}

.addon-alert {
  font-size: 12px;
}

.addon-detection {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 12px;
}

.addon-detection__name {
  font-weight: 500;
}

.addon-detection__meta,
.addon-detection__upload {
  color: var(--n-text-color-3, #999);
}

.addon-upload-meta {
  font-size: 12px;
}

.addon-divider {
  margin: 18px 0 !important;
}

.addon-overrides-table {
  margin-bottom: 8px;
}

.addon-overrides-empty {
  text-align: center;
  padding: 8px;
}

.addon-overrides-footer {
  margin-top: 8px;
}

.addon-actions {
  padding-top: 4px;
}

/* Defaults section — 2-column grid on wide screens, 1-column on
   narrow. Plain <label> renders above each widget for maximum
   predictability (no n-form-item label placement surprises). */
.defaults-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  column-gap: 16px;
  row-gap: 12px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.field-label {
  font-size: 13px;
  color: var(--n-text-color-2, #555);
}
</style>
