<template>
  <n-space vertical :size="16">
    <!-- Host context: detection badge (when available) + overrides hint. -->
    <n-alert
      v-if="hostContext.detected || hostContext.stackName"
      type="success"
      :show-icon="true"
      style="padding: 8px 12px"
    >
      <div style="font-size: 12px">
        <strong>{{ hostContext.detectedLabel }}:</strong>
        {{ hostContext.detected || '—' }}
        <span v-if="hostContext.stackName">
          · {{ t('stack_addon_traefik.ref_stack') }}: <code>{{ hostContext.stackName }}</code>
        </span>
      </div>
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

          <!-- Structured rows -->
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
                :disabled="!nameEditable(row.section)"
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
              <div class="col col-value">
                <n-switch
                  v-if="isBoolRow(row)"
                  :value="row.value === 'true'"
                  size="small"
                  @update:value="setRowField(svc, idx, 'value', $event ? 'true' : 'false')"
                />
                <n-select
                  v-else-if="isMultiValueRow(row)"
                  size="small"
                  multiple
                  filterable
                  tag
                  clearable
                  :value="splitCsv(row.value)"
                  :options="valueOptionsFor(row)"
                  @update:value="setRowField(svc, idx, 'value', joinCsv($event))"
                />
                <n-select
                  v-else-if="hasValueOptions(row)"
                  size="small"
                  filterable
                  tag
                  clearable
                  :value="row.value || null"
                  :options="valueOptionsFor(row)"
                  @update:value="setRowField(svc, idx, 'value', $event || '')"
                />
                <n-input
                  v-else
                  size="small"
                  :value="row.value"
                  :status="validateStatus(row)"
                  :placeholder="valuePlaceholder(svc, row)"
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

    <LabelPreview :labels-by-service="previewLabels" />
  </n-space>
</template>

<script setup lang="ts">
/*
 * StructuredLabelsEditor — shared editor for any addon whose wizard
 * state is a flat label map. Four callers (Traefik / Sablier / Watchtower /
 * Backup) pass their own `schema` describing:
 *   - sections the operator can pick from (router / service / middleware /
 *     enable / custom — catalogs vary per addon);
 *   - for each section, which keys are offered in the dropdown;
 *   - how to assemble the final label (prefix + interpolation of
 *     name/key);
 *   - which rows use bool/multi/select/plain-input widgets;
 *   - which default rows to seed on first-enable.
 *
 * `cfg` is a record keyed by service name with the shape
 * `{ enabled: boolean; labels: Record<string,string> }`. The editor
 * keeps a working copy; Apply flushes to the caller via update:cfg.
 */
import { computed, ref, watch } from 'vue'
import {
  NSpace, NAlert, NSwitch, NSelect, NInput, NTag,
  NCollapse, NCollapseItem, NButton, NIcon,
} from 'naive-ui'
import { AddOutline as AddIcon, TrashOutline as TrashIcon } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import LabelPreview from './LabelPreview.vue'

export interface StructuredRow {
  section: string
  name: string
  key: string
  value: string
}

export interface AddonLabelCfg {
  enabled: boolean
  labels?: Record<string, string>
}

export interface EditorSchema {
  // Label key prefix this addon claims (matches backend addonPrefixes).
  prefix: string
  // Master "enable" label key (e.g. "traefik.enable").
  enableKey: string
  // Section catalogue (dropdown values + human labels).
  sections: { label: string; value: string }[]
  // Keys offered per section.
  keyCatalogue: Record<string, string[]>
  // true when the `name` column is editable for a given section.
  nameEditable: (section: string) => boolean
  // Maps (section, name, key) -> the full traefik.*-like label.
  labelFromRow: (row: StructuredRow) => string
  // Maps a label key -> a structured row (best-effort; fall through
  // to `custom` for unrecognised shapes).
  rowFromLabel: (key: string, value: string) => StructuredRow
  // Widget hints for the value editor.
  isBoolRow?: (row: StructuredRow) => boolean
  isMultiValueRow?: (row: StructuredRow) => boolean
  hasValueOptions?: (row: StructuredRow) => boolean
  valueOptionsFor?: (row: StructuredRow) => { label: string; value: string }[]
  validateStatus?: (row: StructuredRow) => 'success' | 'warning' | undefined
  valuePlaceholder?: (svc: string, row: StructuredRow) => string
  // Starter rows injected on first-enable of a service.
  defaultRowsFor: (svc: string) => StructuredRow[]
}

const props = defineProps<{
  services: string[]
  schema: EditorSchema
  hostContext: {
    detected?: string
    detectedLabel: string
    stackName?: string
    overrides?: Record<string, string>
  }
  modelValue: Record<string, AddonLabelCfg>
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: Record<string, AddonLabelCfg>): void
}>()

const { t } = useI18n()

const overridesEntries = computed(() => {
  const o = props.hostContext.overrides || {}
  return Object.keys(o).map((k) => ({ k, v: o[k] }))
})

// ---- Working state ----------------------------------------------------
const workingEnabled = ref<Record<string, boolean>>({})
const workingRows = ref<Record<string, StructuredRow[]>>({})
const serviceTouched = ref<Record<string, boolean>>({})

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
  const labels = cfg.labels || {}
  const rows: StructuredRow[] = []
  for (const k of Object.keys(labels).sort()) {
    if (!k.startsWith(props.schema.prefix)) continue
    rows.push(props.schema.rowFromLabel(k, labels[k]))
  }
  workingRows.value[svc] = rows
}

function rowCfg(svc: string): { enabled: boolean } {
  return { enabled: !!workingEnabled.value[svc] }
}

function structuredRows(svc: string): StructuredRow[] {
  return workingRows.value[svc] || []
}

function labelCount(svc: string): number {
  const m = props.modelValue[svc]?.labels || {}
  return Object.keys(m).length + (props.modelValue[svc]?.enabled ? 1 : 0)
}

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
    const k = props.schema.labelFromRow(r)
    if (!k) continue
    out[k] = r.value
  }
  return out
}

// ---- Dropdown helpers -------------------------------------------------
const sectionOptions = computed(() => props.schema.sections)

function keyOptionsFor(section: string) {
  const keys = props.schema.keyCatalogue[section] || []
  return keys.map((k) => ({ label: k, value: k }))
}

function keyPlaceholder(section: string): string {
  if (section === 'enable') return props.schema.enableKey
  if (section === 'custom') return t('stack_addon_traefik.custom_key_placeholder')
  return t('stack_addon_traefik.pick_key_placeholder')
}

function namePlaceholder(section: string, svc: string): string {
  if (!props.schema.nameEditable(section)) return '—'
  return t('stack_addon_traefik.name_placeholder', { svc })
}

function nameEditable(section: string): boolean {
  return props.schema.nameEditable(section)
}

function nameOptionsFor(svc: string, section: string) {
  if (!props.schema.nameEditable(section)) return []
  const seen = new Set<string>()
  for (const r of structuredRows(svc)) {
    if (r.section === section && r.name) seen.add(r.name)
  }
  seen.add(svc)
  return Array.from(seen).sort().map((n) => ({ label: n, value: n }))
}

function isBoolRow(row: StructuredRow): boolean {
  return props.schema.isBoolRow ? props.schema.isBoolRow(row) : false
}

function isMultiValueRow(row: StructuredRow): boolean {
  return props.schema.isMultiValueRow ? props.schema.isMultiValueRow(row) : false
}

function hasValueOptions(row: StructuredRow): boolean {
  return props.schema.hasValueOptions ? props.schema.hasValueOptions(row) : false
}

function valueOptionsFor(row: StructuredRow) {
  return props.schema.valueOptionsFor ? props.schema.valueOptionsFor(row) : []
}

function validateStatus(row: StructuredRow): 'success' | 'warning' | undefined {
  return props.schema.validateStatus ? props.schema.validateStatus(row) : undefined
}

function valuePlaceholder(svc: string, row: StructuredRow): string {
  if (props.schema.valuePlaceholder) return props.schema.valuePlaceholder(svc, row)
  return t('stack_addon_traefik.value_placeholder', { svc })
}

function splitCsv(v: string | undefined): string[] {
  if (!v) return []
  return v.split(',').map((s) => s.trim()).filter(Boolean)
}

function joinCsv(arr: unknown): string {
  if (!Array.isArray(arr)) return ''
  return (arr as string[]).filter(Boolean).join(',')
}

// ---- Mutations --------------------------------------------------------
function setEnabled(svc: string, enabled: boolean) {
  workingEnabled.value[svc] = enabled
  serviceTouched.value[svc] = true
  if (enabled && structuredRows(svc).length === 0) {
    addDefaults(svc, false)
  }
}

function addDefaults(svc: string, force: boolean) {
  if (force) workingRows.value[svc] = []
  const rows = structuredRows(svc).slice()
  for (const d of props.schema.defaultRowsFor(svc)) {
    // Skip defaults whose (section, key) is already represented for
    // the same name so Re-apply doesn't duplicate.
    const exists = rows.some(
      (r) => r.section === d.section && r.key === d.key && r.name === d.name,
    )
    if (!exists) rows.push({ ...d })
  }
  workingRows.value[svc] = rows
  serviceTouched.value[svc] = true
}

function addRow(svc: string) {
  const rows = structuredRows(svc).slice()
  const first = props.schema.sections[0]?.value || 'custom'
  rows.push({ section: first, name: props.schema.nameEditable(first) ? svc : '', key: '', value: '' })
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
    row.section = String(value)
    // Reset name/key when the section changes.
    if (row.section === 'enable') {
      row.name = ''
      row.key = props.schema.enableKey
    } else if (row.section === 'custom') {
      row.name = ''
      row.key = ''
    } else {
      row.name = props.schema.nameEditable(row.section) ? svc : ''
      row.key = ''
    }
  } else {
    (row as any)[field] = value
  }
  rows[idx] = row
  workingRows.value[svc] = rows
  serviceTouched.value[svc] = true
}

function applyChanges(svc: string) {
  const next: AddonLabelCfg = {
    enabled: !!workingEnabled.value[svc],
    labels: collectWorkingLabels(svc),
  }
  const payload = { ...props.modelValue }
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

// ---- Preview ----------------------------------------------------------
const previewLabels = computed(() => {
  const out: Record<string, Record<string, string>> = {}
  for (const svc of props.services) {
    const cfg = props.modelValue[svc]
    if (!cfg) continue
    const labels: Record<string, string> = { ...(cfg.labels || {}) }
    if (cfg.enabled) labels[props.schema.enableKey] = 'true'
    if (Object.keys(labels).length > 0) out[svc] = labels
  }
  return out
})

const initiallyExpanded = computed(() =>
  props.services.filter((s) => {
    const cfg = props.modelValue[s]
    return !!cfg && (cfg.enabled || Object.keys(cfg.labels || {}).length > 0)
  }),
)
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
