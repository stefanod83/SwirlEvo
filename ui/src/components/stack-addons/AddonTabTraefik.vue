<template>
  <n-space vertical :size="16">
    <!-- Host-level context badges. The detection block remains quietly
         informational — nothing interactive here, all the config lives
         in the per-service labels editor below. -->
    <n-alert
      v-if="discovery || hostRefs"
      type="success"
      :show-icon="true"
      style="padding: 8px 12px"
    >
      <n-space vertical :size="4">
        <div style="font-size: 12px">
          <strong>{{ t('stack_addon_traefik.detected') }}:</strong>
          <span v-if="discovery?.containerName"> {{ discovery.containerName }}</span>
          <span v-else-if="hostRefs?.containerName"> {{ hostRefs.containerName }}</span>
          <span v-else class="muted"> —</span>
          <span v-if="discovery?.image"> · {{ discovery.image }}</span>
          <span v-if="discovery?.version"> · {{ discovery.version }}</span>
        </div>
        <div v-if="hostRefs?.stackName" class="muted" style="font-size: 12px">
          {{ t('stack_addon_traefik.ref_stack') }}: <code>{{ hostRefs.stackName }}</code>
        </div>
      </n-space>
    </n-alert>
    <n-alert v-else type="warning" :show-icon="true" style="padding: 8px 12px">
      {{ t('stack_addon_traefik.not_detected_hint') }}
    </n-alert>

    <n-alert
      v-if="overridesEntries.length"
      type="info"
      :show-icon="false"
      style="padding: 8px 12px"
    >
      <div style="font-size: 12px; margin-bottom: 4px">
        {{ t('stack_addon_traefik.overrides_title') }}
      </div>
      <n-space :size="4">
        <n-tag v-for="e of overridesEntries" :key="e.k" size="small">
          {{ e.k }}=<code>{{ e.v }}</code>
        </n-tag>
      </n-space>
    </n-alert>

    <n-alert v-if="!services.length" type="info" :show-icon="true">
      {{ t('stack_addon_traefik.no_services') }}
    </n-alert>

    <!-- Per-service editor. One collapsible card per service; each
         card carries the master enable toggle, the structured row
         editor, and (collapsed) the raw passthrough fallback. -->
    <n-collapse v-else :default-expanded-names="initiallyExpanded" accordion>
      <n-collapse-item
        v-for="svc of services"
        :key="svc"
        :name="svc"
      >
        <template #header>
          <n-space :size="8" align="center">
            <code class="svc-name">{{ svc }}</code>
            <n-tag
              v-if="rowCfg(svc).enabled"
              size="small"
              type="success"
            >{{ t('stack_addon_traefik.enabled') }}</n-tag>
            <n-tag
              v-if="labelCount(svc)"
              size="tiny"
              round
            >{{ labelCount(svc) }}</n-tag>
            <n-tag
              v-if="isDirty(svc)"
              size="tiny"
              type="warning"
              round
            >{{ t('stack_addon_traefik.dirty') }}</n-tag>
          </n-space>
        </template>

        <n-space vertical :size="12">
          <!-- Master enable -->
          <n-space align="center" :size="12">
            <span>{{ t('stack_addon_traefik.enable') }}</span>
            <n-switch :value="rowCfg(svc).enabled" @update:value="setEnabled(svc, $event)" />
            <span v-if="rowCfg(svc).enabled" class="muted" style="font-size: 12px">
              {{ t('stack_addon_traefik.enable_hint') }}
            </span>
          </n-space>

          <!-- Structured rows: [section · name · key · value] -->
          <div class="structured-editor">
            <div class="structured-header">
              <span class="col col-section">{{ t('stack_addon_traefik.row_section') }}</span>
              <span class="col col-name">{{ t('stack_addon_traefik.row_name') }}</span>
              <span class="col col-key">{{ t('stack_addon_traefik.row_key') }}</span>
              <span class="col col-value">{{ t('stack_addon_traefik.row_value') }}</span>
              <span class="col col-actions"></span>
            </div>

            <div
              v-for="(row, idx) of structuredRows(svc)"
              :key="svc + '-' + idx"
              class="structured-row"
            >
              <n-select
                size="small"
                :value="row.section"
                :options="sectionOptions"
                class="col col-section"
                @update:value="setRowField(svc, idx, 'section', $event)"
              />
              <n-select
                size="small"
                :value="row.name"
                :options="nameOptionsFor(svc, row.section)"
                :placeholder="namePlaceholder(row.section, svc)"
                :disabled="row.section === 'enable' || row.section === 'custom'"
                filterable
                tag
                class="col col-name"
                @update:value="setRowField(svc, idx, 'name', $event || '')"
              />
              <n-select
                size="small"
                :value="row.key"
                :options="keyOptionsFor(row.section)"
                :placeholder="keyPlaceholder(row.section)"
                :disabled="row.section === 'enable'"
                filterable
                tag
                clearable
                class="col col-key"
                @update:value="setRowField(svc, idx, 'key', $event || '')"
              />
              <!--
                Value editor — a concrete widget per (section, key) so
                the bidirectional cast between `row.value: string` and
                the widget's native type is explicit. Wrapping in
                <component :is> with v-bind hid subtle mismatches
                (n-switch strict boolean compare, n-select multiple
                wanting array, n-input-number wanting number) that made
                tls/middleware/port non-interactive.
              -->
              <div class="col col-value">
                <n-switch
                  v-if="isBoolRow(row)"
                  :value="row.value === 'true'"
                  size="small"
                  @update:value="setRowField(svc, idx, 'value', $event ? 'true' : 'false')"
                />
                <n-select
                  v-else-if="row.section === 'router' && row.key === 'middlewares'"
                  size="small"
                  multiple
                  filterable
                  tag
                  clearable
                  :value="splitCsv(row.value)"
                  :options="buildOptionList(discovery?.middlewares, hostRefs?.middlewares)"
                  @update:value="setRowField(svc, idx, 'value', joinCsv($event))"
                />
                <n-select
                  v-else-if="row.section === 'router' && row.key === 'entrypoints'"
                  size="small"
                  filterable
                  tag
                  clearable
                  :value="row.value || null"
                  :options="buildOptionList(discovery?.entryPoints, hostRefs?.entryPoints)"
                  @update:value="setRowField(svc, idx, 'value', $event || '')"
                />
                <n-select
                  v-else-if="row.section === 'router' && row.key === 'tls.certresolver'"
                  size="small"
                  filterable
                  tag
                  clearable
                  :value="row.value || null"
                  :options="buildOptionList(discovery?.certResolvers, hostRefs?.certResolvers)"
                  @update:value="setRowField(svc, idx, 'value', $event || '')"
                />
                <n-input
                  v-else
                  size="small"
                  :value="row.value"
                  :status="portStatus(row)"
                  :placeholder="isPortRow(row) ? '80' : t('stack_addon_traefik.value_placeholder', { svc })"
                  @update:value="setRowField(svc, idx, 'value', $event)"
                />
              </div>
              <n-button
                class="col col-actions"
                size="tiny"
                quaternary
                type="error"
                @click="removeRow(svc, idx)"
              >
                <template #icon>
                  <n-icon><trash-icon /></n-icon>
                </template>
              </n-button>
            </div>

            <n-space :size="8" style="margin-top: 8px">
              <n-button size="small" quaternary @click="addRow(svc)">
                <template #icon>
                  <n-icon><add-icon /></n-icon>
                </template>
                {{ t('stack_addon_traefik.add_row') }}
              </n-button>
              <n-button
                size="small"
                quaternary
                :disabled="!rowCfg(svc).enabled"
                @click="addDefaults(svc, true)"
              >
                {{ t('stack_addon_traefik.reset_to_defaults') }}
              </n-button>
            </n-space>
          </div>

          <!-- Raw passthrough: everything the structured editor can't
               cleanly encode (verbatim pastes, key patterns we don't
               offer in dropdowns). Kept explicitly secondary. -->
          <n-collapse>
            <n-collapse-item :name="'raw-' + svc">
              <template #header>
                <n-space :size="6" align="center">
                  <n-tag size="tiny" type="info">raw</n-tag>
                  <span style="font-size: 12px">{{ t('stack_addon_traefik.raw_title') }}</span>
                  <n-tag v-if="rawRows(svc).length" size="tiny" round>{{ rawRows(svc).length }}</n-tag>
                </n-space>
              </template>
              <n-table size="small" :bordered="true" :single-line="false">
                <thead>
                  <tr>
                    <th style="width: 50%">{{ t('fields.key') }}</th>
                    <th>{{ t('fields.value') }}</th>
                    <th style="width: 60px"></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(r, i) of rawRows(svc)" :key="svc + '-raw-' + i">
                    <td><n-input size="small" :value="r.k" @update:value="setRawKey(svc, i, $event)" /></td>
                    <td><n-input size="small" :value="r.v" @update:value="setRawValue(svc, i, $event)" /></td>
                    <td>
                      <n-button size="tiny" quaternary type="error" @click="removeRaw(svc, i)">
                        {{ t('buttons.delete') }}
                      </n-button>
                    </td>
                  </tr>
                  <tr v-if="!rawRows(svc).length">
                    <td colspan="3" style="text-align: center; padding: 8px" class="muted">
                      {{ t('stack_addon_traefik.raw_empty') }}
                    </td>
                  </tr>
                </tbody>
              </n-table>
              <n-button size="small" quaternary style="margin-top: 8px" @click="addRaw(svc)">
                <template #icon>
                  <n-icon><add-icon /></n-icon>
                </template>
                {{ t('stack_addon_traefik.add_raw') }}
              </n-button>
            </n-collapse-item>
          </n-collapse>

          <!-- Apply changes: structured + raw → cfg.labels. Without
               this the model stays dirty and the preview doesn't
               update. Deliberately explicit so operators can revise
               freely before the YAML reflects anything. -->
          <n-space justify="end" :size="8">
            <n-button
              size="small"
              :disabled="!isDirty(svc)"
              @click="revertChanges(svc)"
            >
              {{ t('stack_addon_traefik.revert') }}
            </n-button>
            <n-button
              size="small"
              type="primary"
              :disabled="!isDirty(svc)"
              @click="applyChanges(svc)"
            >
              {{ t('stack_addon_traefik.apply') }}
            </n-button>
          </n-space>
        </n-space>
      </n-collapse-item>
    </n-collapse>

    <!-- Applied state preview. The builder mirrors what the backend
         will write on save; updates only after Apply. -->
    <LabelPreview :labels-by-service="previewLabels" />
  </n-space>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NSpace, NAlert, NTable, NSwitch, NSelect, NInput, NTag,
  NCollapse, NCollapseItem, NButton, NIcon,
} from 'naive-ui'
import {
  AddOutline as AddIcon,
  TrashOutline as TrashIcon,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import LabelPreview from './LabelPreview.vue'
import type { TraefikAddon, TraefikServiceCfg } from '@/api/compose_stack'
import type { TraefikExtract } from '@/api/host'
import { buildTraefikLabels } from '@/utils/stack-addon-labels'

// StructuredRow is the UI-side unit for one traefik.* label in the
// guided editor. Flattened to a label key at apply time via
// labelFromRow(); reverse-parsed from a label key via rowFromLabel().
interface StructuredRow {
  section: 'enable' | 'router' | 'service' | 'middleware' | 'custom'
  name: string
  key: string
  value: string
}

const props = defineProps<{
  services: string[]
  discovery: TraefikAddon | null
  hostRefs: (TraefikExtract & { stackName?: string }) | null
  mode: 'swarm' | 'standalone'
  modelValue: Record<string, TraefikServiceCfg>
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: Record<string, TraefikServiceCfg>): void
}>()

const { t } = useI18n()

// Overrides surfaced from Host edit page — read-only hints.
const overridesEntries = computed(() => {
  const o = props.hostRefs?.overrides || {}
  return Object.keys(o).map((k) => ({ k, v: o[k] }))
})

// ----------------------------------------------------------------------
// Per-service working state
//
// The tab keeps TWO maps per service:
//   - workingRows      : the structured editor rows the user is editing;
//   - workingRaw       : raw key/value pairs typed in the fallback;
//   - workingEnabled   : master toggle.
// `modelValue` is the APPLIED state: the preview + save payload come
// from there. apply() flushes working-* into modelValue.labels; revert()
// rebuilds working from modelValue. isDirty() compares the two.
//
// This keeps the UX predictable: typing doesn't flicker the preview,
// and the operator stays in control of when YAML state changes.
// ----------------------------------------------------------------------

const workingEnabled = ref<Record<string, boolean>>({})
const workingRows = ref<Record<string, StructuredRow[]>>({})
const workingRaw = ref<Record<string, { k: string; v: string }[]>>({})
// serviceTouched tracks services the operator has explicitly enabled in
// this session so we know when to seed defaults. Without this, services
// whose modelValue already carries enabled=true + labels would get seeded
// again every time the props change.
const serviceTouched = ref<Record<string, boolean>>({})

// Keep working state in sync with modelValue changes coming from the
// outside (e.g. stack load, version restore). We only rebuild a service
// that hasn't been touched in this session — never clobber the user's
// in-flight edits.
watch(
  () => props.modelValue,
  (v) => {
    for (const svc of Object.keys(v || {})) {
      if (!serviceTouched.value[svc]) rehydrate(svc)
    }
    for (const svc of props.services) {
      if (workingEnabled.value[svc] === undefined && !serviceTouched.value[svc]) {
        rehydrate(svc)
      }
    }
  },
  { immediate: true, deep: true },
)

watch(
  () => props.services,
  (svcs) => {
    for (const svc of svcs) {
      if (workingEnabled.value[svc] === undefined) rehydrate(svc)
    }
  },
  { immediate: true },
)

function rehydrate(svc: string) {
  const cfg = props.modelValue[svc] || { enabled: false }
  workingEnabled.value[svc] = !!cfg.enabled
  const { structured, raw } = splitLabels(cfg.labels || {})
  workingRows.value[svc] = structured
  workingRaw.value[svc] = raw
}

function rowCfg(svc: string): { enabled: boolean } {
  return { enabled: !!workingEnabled.value[svc] }
}

function structuredRows(svc: string): StructuredRow[] {
  return workingRows.value[svc] || []
}

function rawRows(svc: string): { k: string; v: string }[] {
  return workingRaw.value[svc] || []
}

function labelCount(svc: string): number {
  const m = props.modelValue[svc]?.labels || {}
  return Object.keys(m).length + (props.modelValue[svc]?.enabled ? 1 : 0)
}

// ----------------------------------------------------------------------
// Dirty tracking
// ----------------------------------------------------------------------

function isDirty(svc: string): boolean {
  const current = props.modelValue[svc] || { enabled: false }
  if (!!current.enabled !== !!workingEnabled.value[svc]) return true
  const applied = current.labels || {}
  const next = collectWorkingLabels(svc)
  if (Object.keys(applied).length !== Object.keys(next).length) return true
  for (const k of Object.keys(next)) {
    if (applied[k] !== next[k]) return true
  }
  return false
}

function collectWorkingLabels(svc: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const r of structuredRows(svc)) {
    const k = labelFromRow(r)
    if (!k) continue
    out[k] = r.value
  }
  for (const r of rawRows(svc)) {
    const k = r.k.trim()
    if (!k) continue
    out[k] = r.v
  }
  return out
}

// ----------------------------------------------------------------------
// Section/name/key catalogue — drives the dropdowns
// ----------------------------------------------------------------------

const sectionOptions = [
  { label: 'router', value: 'router' },
  { label: 'service', value: 'service' },
  { label: 'middleware', value: 'middleware' },
  { label: 'traefik.enable', value: 'enable' },
  { label: 'custom', value: 'custom' },
]

const ROUTER_KEYS = [
  'rule', 'entrypoints', 'priority', 'service', 'middlewares',
  'tls', 'tls.certresolver', 'tls.options', 'tls.domains[0].main',
  'tls.domains[0].sans',
]
const SERVICE_KEYS = [
  'loadbalancer.server.port',
  'loadbalancer.server.scheme',
  'loadbalancer.passhostheader',
  'loadbalancer.sticky.cookie.name',
  'loadbalancer.healthcheck.path',
  'loadbalancer.healthcheck.interval',
]
const MIDDLEWARE_KEYS = [
  'basicauth.users',
  'basicauth.realm',
  'stripprefix.prefixes',
  'redirectregex.regex',
  'redirectregex.replacement',
  'redirectscheme.scheme',
  'ratelimit.average',
  'headers.customresponseheaders.X-Frame-Options',
  'headers.sslredirect',
  'compress',
]

function keyOptionsFor(section: StructuredRow['section']) {
  switch (section) {
    case 'router':     return ROUTER_KEYS.map((k) => ({ label: k, value: k }))
    case 'service':    return SERVICE_KEYS.map((k) => ({ label: k, value: k }))
    case 'middleware': return MIDDLEWARE_KEYS.map((k) => ({ label: k, value: k }))
    case 'custom':     return []
    default:           return []
  }
}

function keyPlaceholder(section: StructuredRow['section']): string {
  if (section === 'enable') return 'traefik.enable'
  if (section === 'custom') return t('stack_addon_traefik.custom_key_placeholder')
  return t('stack_addon_traefik.pick_key_placeholder')
}

function namePlaceholder(section: StructuredRow['section'], svc: string): string {
  if (section === 'enable' || section === 'custom') return '—'
  return t('stack_addon_traefik.name_placeholder', { svc })
}

function nameOptionsFor(svc: string, section: StructuredRow['section']) {
  if (section === 'enable' || section === 'custom') return []
  const seen = new Set<string>()
  const rows = structuredRows(svc)
  for (const r of rows) {
    if (r.section === section && r.name) seen.add(r.name)
  }
  // Seed with service name as a default hint.
  if (section === 'router') seen.add(svc)
  if (section === 'service') seen.add(svc)
  return Array.from(seen).sort().map((n) => ({ label: n, value: n }))
}

// Row-type classifiers — used by the template's v-if ladder so each
// widget binds to `row.value: string` with its own explicit cast.

function isBoolRow(row: StructuredRow): boolean {
  if (row.section === 'enable') return true
  if (row.section === 'router' && row.key === 'tls') return true
  return false
}

function isPortRow(row: StructuredRow): boolean {
  return row.section === 'service' && row.key === 'loadbalancer.server.port'
}

// portStatus feeds n-input's status prop so non-numeric entries show
// a soft warning (not a hard error — Traefik accepts {port},
// environment refs via ${VAR}, named ports, etc., so we validate
// gently). undefined = neutral, no styling.
function portStatus(row: StructuredRow): 'success' | 'warning' | undefined {
  if (!isPortRow(row)) return undefined
  const v = (row.value || '').trim()
  if (!v) return undefined
  // Accept pure integers 1..65535 as "success"; everything else we
  // flag as "warning" so operators notice but can still save (e.g.
  // `${APP_PORT}` is a legitimate value).
  if (/^[0-9]+$/.test(v)) {
    const n = parseInt(v, 10)
    return n >= 1 && n <= 65535 ? 'success' : 'warning'
  }
  return 'warning'
}

// splitCsv / joinCsv cross the boundary between the label's
// comma-separated string value and n-select's array model. Empty
// input → empty array (not a one-element array of "").
function splitCsv(v: string | undefined): string[] {
  if (!v) return []
  return v.split(',').map((s) => s.trim()).filter(Boolean)
}

function joinCsv(arr: unknown): string {
  if (!Array.isArray(arr)) return ''
  return (arr as string[]).filter(Boolean).join(',')
}

function buildOptionList(
  docker: { name: string; origin: string }[] | undefined,
  file: string[] | undefined,
) {
  const seen = new Set<string>()
  const out: { label: string; value: string }[] = []
  for (const d of docker || []) {
    if (!d.name || seen.has(d.name)) continue
    seen.add(d.name)
    out.push({ label: `${d.name} · docker`, value: d.name })
  }
  for (const n of file || []) {
    if (!n || seen.has(n)) continue
    seen.add(n)
    out.push({ label: `${n} · host`, value: n })
  }
  return out
}

// ----------------------------------------------------------------------
// Row conversions
// ----------------------------------------------------------------------

// labelFromRow flattens a StructuredRow into a traefik.* label key.
// Returns "" when the row is incomplete (UI shows a dirty state; no
// label is emitted yet).
function labelFromRow(r: StructuredRow): string {
  switch (r.section) {
    case 'enable':     return r.value === 'true' || r.value === true as any ? 'traefik.enable' : ''
    case 'router':
      if (!r.name || !r.key) return ''
      return `traefik.http.routers.${r.name}.${r.key}`
    case 'service':
      if (!r.name || !r.key) return ''
      return `traefik.http.services.${r.name}.${r.key}`
    case 'middleware':
      if (!r.name || !r.key) return ''
      return `traefik.http.middlewares.${r.name}.${r.key}`
    case 'custom':
      // custom rows let the operator type the full key verbatim
      return r.key.trim()
  }
  return ''
}

// rowFromLabel is the inverse: given a traefik.* label key decide which
// (section, name, key) slice it belongs to. Unknown shapes fall back
// to 'custom' so nothing gets lost.
function rowFromLabel(k: string, v: string): StructuredRow {
  if (k === 'traefik.enable') {
    return { section: 'enable', name: '', key: 'traefik.enable', value: v }
  }
  const routerPrefix = 'traefik.http.routers.'
  const servicePrefix = 'traefik.http.services.'
  const middlewarePrefix = 'traefik.http.middlewares.'
  const matchPrefix = (p: string, section: StructuredRow['section']): StructuredRow | null => {
    if (!k.startsWith(p)) return null
    const rest = k.slice(p.length)
    const dot = rest.indexOf('.')
    if (dot <= 0) return null
    return { section, name: rest.slice(0, dot), key: rest.slice(dot + 1), value: v }
  }
  return (
    matchPrefix(routerPrefix, 'router') ||
    matchPrefix(servicePrefix, 'service') ||
    matchPrefix(middlewarePrefix, 'middleware') ||
    { section: 'custom', name: '', key: k, value: v }
  )
}

// splitLabels divides the service's labels into "structured" rows (the
// ones whose key matches a known Traefik section) and "raw" rows
// (kept only in the raw fallback — currently unused, since the
// catch-all "custom" section absorbs unknown keys). Future-proof: if
// we later draw a finer line, the logic below is the single choke.
function splitLabels(labels: Record<string, string>): {
  structured: StructuredRow[]
  raw: { k: string; v: string }[]
} {
  const structured: StructuredRow[] = []
  const raw: { k: string; v: string }[] = []
  for (const k of Object.keys(labels).sort()) {
    const v = labels[k]
    if (k.startsWith('traefik.')) {
      structured.push(rowFromLabel(k, v))
    } else {
      raw.push({ k, v })
    }
  }
  return { structured, raw }
}

// ----------------------------------------------------------------------
// Mutations
// ----------------------------------------------------------------------

function setEnabled(svc: string, enabled: boolean) {
  workingEnabled.value[svc] = enabled
  serviceTouched.value[svc] = true
  // First-time enable on a service with no rows yet: seed defaults.
  if (enabled && structuredRows(svc).length === 0) {
    addDefaults(svc, false)
  }
}

// addDefaults populates the structured editor with a sensible starter
// set: router/rule = Host(defaultDomain || placeholder), entrypoint =
// defaultEntrypoint (from host), service/port placeholder, tls=true,
// tls.certresolver = defaultCertResolver, router.middlewares =
// defaultMiddleware. force=true wipes existing rows.
function addDefaults(svc: string, force: boolean) {
  if (force) workingRows.value[svc] = []
  const rows = structuredRows(svc).slice()
  const h = props.hostRefs
  const hasRow = (section: StructuredRow['section'], key: string) =>
    rows.some((r) => r.section === section && r.key === key && (!section.includes('router') || r.name === svc))
  const push = (section: StructuredRow['section'], name: string, key: string, value: string) => {
    rows.push({ section, name, key, value })
  }
  if (!hasRow('router', 'rule')) {
    const domain = h?.defaultDomain || 'example.com'
    push('router', svc, 'rule', `Host(\`${domain}\`)`)
  }
  if (!hasRow('router', 'entrypoints') && h?.defaultEntrypoint) {
    push('router', svc, 'entrypoints', h.defaultEntrypoint)
  }
  if (!hasRow('router', 'tls')) {
    push('router', svc, 'tls', 'true')
  }
  if (!hasRow('router', 'tls.certresolver') && h?.defaultCertResolver) {
    push('router', svc, 'tls.certresolver', h.defaultCertResolver)
  }
  if (!hasRow('router', 'middlewares') && h?.defaultMiddleware) {
    push('router', svc, 'middlewares', h.defaultMiddleware)
  }
  if (!hasRow('service', 'loadbalancer.server.port')) {
    push('service', svc, 'loadbalancer.server.port', '')
  }
  workingRows.value[svc] = rows
}

function addRow(svc: string) {
  const rows = structuredRows(svc).slice()
  rows.push({ section: 'router', name: svc, key: '', value: '' })
  workingRows.value[svc] = rows
  serviceTouched.value[svc] = true
}

function removeRow(svc: string, idx: number) {
  const rows = structuredRows(svc).slice()
  rows.splice(idx, 1)
  workingRows.value[svc] = rows
  serviceTouched.value[svc] = true
}

function setRowField(svc: string, idx: number, field: keyof StructuredRow, value: any) {
  const rows = structuredRows(svc).slice()
  if (idx < 0 || idx >= rows.length) return
  const row = { ...rows[idx] }
  if (field === 'section') {
    row.section = value
    // Reset name/key when switching section — the previous values make
    // no sense in the new namespace.
    if (value === 'enable') { row.name = ''; row.key = 'traefik.enable' }
    else if (value === 'custom') { row.name = ''; row.key = '' }
    else { row.name = svc; row.key = '' }
  } else if (field === 'section' as any) {
    (row as any)[field] = value
  } else {
    (row as any)[field] = value
  }
  rows[idx] = row
  workingRows.value[svc] = rows
  serviceTouched.value[svc] = true
}

// Raw passthrough helpers mirror the structured side but skip the
// section/name decomposition — keys are typed verbatim.
function addRaw(svc: string) {
  const rows = rawRows(svc).slice()
  rows.push({ k: '', v: '' })
  workingRaw.value[svc] = rows
  serviceTouched.value[svc] = true
}

function removeRaw(svc: string, idx: number) {
  const rows = rawRows(svc).slice()
  rows.splice(idx, 1)
  workingRaw.value[svc] = rows
  serviceTouched.value[svc] = true
}

function setRawKey(svc: string, idx: number, newKey: string) {
  const rows = rawRows(svc).slice()
  if (idx < 0 || idx >= rows.length) return
  rows[idx] = { ...rows[idx], k: newKey }
  workingRaw.value[svc] = rows
  serviceTouched.value[svc] = true
}

function setRawValue(svc: string, idx: number, newVal: string) {
  const rows = rawRows(svc).slice()
  if (idx < 0 || idx >= rows.length) return
  rows[idx] = { ...rows[idx], v: newVal }
  workingRaw.value[svc] = rows
  serviceTouched.value[svc] = true
}

// ----------------------------------------------------------------------
// Apply / revert — flush working state into modelValue
// ----------------------------------------------------------------------

function applyChanges(svc: string) {
  const next: TraefikServiceCfg = {
    enabled: !!workingEnabled.value[svc],
    labels: collectWorkingLabels(svc),
  }
  const payload = { ...props.modelValue }
  // Drop the entry entirely when wizard disabled AND no labels — keeps
  // the save payload compact.
  if (!next.enabled && !Object.keys(next.labels || {}).length) {
    delete payload[svc]
  } else {
    payload[svc] = next
  }
  emit('update:modelValue', payload)
  serviceTouched.value[svc] = false
}

function revertChanges(svc: string) {
  serviceTouched.value[svc] = false
  rehydrate(svc)
}

// ----------------------------------------------------------------------
// Preview (reflects applied state, not working state)
// ----------------------------------------------------------------------

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

// Expand services that currently carry wizard state so operators land
// on something meaningful. Services configured later stay collapsed.
const initiallyExpanded = computed(() => {
  return props.services.filter((s) => {
    const cfg = props.modelValue[s]
    return !!cfg && (cfg.enabled || Object.keys(cfg.labels || {}).length > 0)
  })
})
</script>

<style scoped>
.muted { color: var(--n-text-color-3, #999); }

.svc-name {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
}

.structured-editor {
  border: 1px solid var(--n-border-color, rgba(128, 128, 128, 0.18));
  border-radius: 4px;
  padding: 8px 10px;
  background: rgba(128, 128, 128, 0.04);
}

.structured-header,
.structured-row {
  display: grid;
  grid-template-columns: 140px 1fr 1fr 1.5fr 40px;
  gap: 8px;
  align-items: center;
}

.structured-header {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  opacity: 0.65;
  padding: 4px 0;
  border-bottom: 1px solid var(--n-border-color, rgba(128, 128, 128, 0.18));
  margin-bottom: 8px;
}

.structured-row {
  padding: 4px 0;
}

.col {
  min-width: 0;
}
</style>
