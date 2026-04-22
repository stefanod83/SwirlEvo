<template>
  <n-space vertical :size="12">
    <!-- Discovery badge: dotted line between the detected Traefik
         container (docker inspect), the host-curated pointers (stack of
         reference, explicit container name set in Host edit) and the
         provenance of the uploaded config file. -->
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
        <div v-if="hostRefs" class="muted" style="font-size: 12px">
          <span v-if="hostRefs.stackName">
            {{ t('stack_addon_traefik.ref_stack') }}: <code>{{ hostRefs.stackName }}</code>
          </span>
          <span v-if="hostRefs.sourceFile"> · 📄 {{ hostRefs.sourceFile }}</span>
        </div>
      </n-space>
    </n-alert>
    <n-alert v-else type="warning" :show-icon="true" style="padding: 8px 12px">
      {{ t('stack_addon_traefik.not_detected_hint') }}
    </n-alert>

    <!-- Overrides hint: free-form key/value pairs set in the Host page
         are surfaced here as a read-only chip list so the operator knows
         what "default assumptions" the wizard is relying on. -->
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
              :placeholder="hostRefs?.defaultDomain || 'app.example.com'"
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

    <!--
      Passthrough labels — every traefik.* label the wizard can't model
      natively (extra routers, tls.options, middleware@file, plugins,
      ...) is round-tripped through this panel. Key/value editable so
      operators can tweak without leaving the tab; removed entries are
      purged on the next save.
    -->
    <x-panel
      v-if="servicesWithExtras.length"
      :title="t('stack_addon_traefik.extras_title')"
      :subtitle="t('stack_addon_traefik.extras_subtitle')"
      divider="bottom"
    >
      <n-collapse>
        <n-collapse-item
          v-for="svc of servicesWithExtras"
          :key="svc"
          :name="svc"
        >
          <template #header>
            <n-space :size="6" align="center">
              <code>{{ svc }}</code>
              <n-tag size="tiny" round>{{ extrasCount(svc) }}</n-tag>
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
              <tr v-for="(row, idx) of extrasRows(svc)" :key="svc + '-' + idx">
                <td><n-input size="small" :value="row.k" @update:value="setExtraKey(svc, idx, $event)" /></td>
                <td><n-input size="small" :value="row.v" @update:value="setExtraValue(svc, idx, $event)" /></td>
                <td>
                  <n-button size="tiny" quaternary type="error" @click="removeExtra(svc, idx)">
                    {{ t('buttons.delete') }}
                  </n-button>
                </td>
              </tr>
              <tr v-if="!extrasRows(svc).length">
                <td colspan="3" style="text-align: center; padding: 8px;" class="muted">
                  {{ t('stack_addon_traefik.extras_empty') }}
                </td>
              </tr>
            </tbody>
          </n-table>
          <n-button size="small" quaternary @click="addExtra(svc)" style="margin-top: 8px">
            <template #icon>
              <n-icon><add-icon /></n-icon>
            </template>
            {{ t('stack_addon_traefik.add_extra') }}
          </n-button>
        </n-collapse-item>
      </n-collapse>
    </x-panel>

    <!-- Live preview of the labels the backend will emit. -->
    <LabelPreview :labels-by-service="previewLabels" />
  </n-space>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import {
  NSpace, NAlert, NTable, NSwitch, NSelect, NInput, NInputNumber, NTag,
  NCollapse, NCollapseItem, NButton, NIcon,
} from 'naive-ui'
import { AddOutline as AddIcon } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import XPanel from '@/components/Panel.vue'
import LabelPreview from './LabelPreview.vue'
import type { TraefikAddon, TraefikServiceCfg } from '@/api/compose_stack'
import type { TraefikExtract } from '@/api/host'
import { buildTraefikLabels } from '@/utils/stack-addon-labels'

// hostRefs is the Traefik subtree of the Host.AddonConfigExtract. Curated
// from the Host edit page — read-only here. When absent, the tab falls
// back to docker-inspect discovery + generic defaults.
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

const ruleTypeOptions = [
  { label: 'Host(`…`)', value: 'Host' },
  { label: 'PathPrefix(`…`)', value: 'PathPrefix' },
  { label: 'Host + PathPrefix', value: 'Host+PathPrefix' },
]

const overridesEntries = computed(() => {
  const o = props.hostRefs?.overrides || {}
  return Object.keys(o).map((k) => ({ k, v: o[k] }))
})

const entrypointOptions = computed(() => mergeOptions(
  props.discovery?.entryPoints,
  props.hostRefs?.entryPoints,
))
const certResolverOptions = computed(() => mergeOptions(
  props.discovery?.certResolvers,
  props.hostRefs?.certResolvers,
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
    out.push({ label: `${n} · host`, value: n })
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
  if (!next[svc].router) next[svc].router = svc
  // Inherit from host-curated defaults only when the operator hasn't
  // typed anything yet — we NEVER override an explicit user input.
  if (props.hostRefs) {
    if (!next[svc].entrypoint && props.hostRefs.defaultEntrypoint) {
      next[svc].entrypoint = props.hostRefs.defaultEntrypoint
    }
    if (!next[svc].certResolver && props.hostRefs.defaultCertResolver) {
      next[svc].certResolver = props.hostRefs.defaultCertResolver
    }
  }
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

// ---- Passthrough (extraLabels) -------------------------------------------
// Every row in the main table maps to the wizard-modeled subset; advanced
// traefik.* labels live here verbatim. Editable so operators can tweak
// hand-authored config without leaving the tab.

const servicesWithExtras = computed(() => {
  // Include any service that has a cfg entry (even wizard-disabled) —
  // the operator may want to edit passthrough for services not managed
  // by the wizard.
  return props.services.filter((s) => {
    const cfg = props.modelValue[s]
    if (!cfg) return false
    return !!cfg.extraLabels && Object.keys(cfg.extraLabels).length > 0
  })
})

function extrasCount(svc: string): number {
  const m = props.modelValue[svc]?.extraLabels || {}
  return Object.keys(m).length
}

// extrasRows renders the map as an array of {k,v} for the editable table.
// We don't keep a parallel array in reactive state because Vue's map
// iteration doesn't preserve insertion order reliably — sort keys so the
// UI is stable across re-renders.
function extrasRows(svc: string): { k: string; v: string }[] {
  const m = props.modelValue[svc]?.extraLabels || {}
  return Object.keys(m).sort().map((k) => ({ k, v: m[k] }))
}

function writeExtras(svc: string, next: Record<string, string>) {
  const current = { ...(props.modelValue[svc] || { enabled: false }) }
  current.extraLabels = next
  const out = { ...props.modelValue, [svc]: current }
  emit('update:modelValue', out)
}

function setExtraKey(svc: string, idx: number, newKey: string) {
  const rows = extrasRows(svc)
  if (idx < 0 || idx >= rows.length) return
  const oldKey = rows[idx].k
  const next: Record<string, string> = {}
  for (const r of rows) {
    if (r.k === oldKey) next[newKey] = r.v
    else next[r.k] = r.v
  }
  writeExtras(svc, next)
}

function setExtraValue(svc: string, idx: number, newVal: string) {
  const rows = extrasRows(svc)
  if (idx < 0 || idx >= rows.length) return
  const next: Record<string, string> = {}
  for (const r of rows) next[r.k] = r.k === rows[idx].k ? newVal : r.v
  writeExtras(svc, next)
}

function removeExtra(svc: string, idx: number) {
  const rows = extrasRows(svc)
  if (idx < 0 || idx >= rows.length) return
  const keyToDrop = rows[idx].k
  const next: Record<string, string> = {}
  for (const r of rows) if (r.k !== keyToDrop) next[r.k] = r.v
  writeExtras(svc, next)
}

function addExtra(svc: string) {
  const current = props.modelValue[svc]?.extraLabels || {}
  // Insert a placeholder; operator renames the empty key next.
  const next = { ...current, '': '' }
  writeExtras(svc, next)
}
</script>

<style scoped>
.muted { color: var(--n-text-color-3, #999); }
</style>
