<template>
  <StructuredLabelsEditor
    :services="services"
    :schema="schema"
    :host-context="hostContext"
    :model-value="modelValue"
    @update:model-value="emit('update:modelValue', $event)"
  />
</template>

<script setup lang="ts">
// Thin wrapper around StructuredLabelsEditor — all the Traefik-specific
// catalog (sections, key list, default seeds) lives in
// utils/stack-addon-schemas.ts so the other addon tabs can follow the
// same pattern without code duplication.
import { computed } from 'vue'
import StructuredLabelsEditor from './StructuredLabelsEditor.vue'
import type { TraefikAddon, TraefikServiceCfg } from '@/api/compose_stack'
import type { TraefikExtract } from '@/api/host'
import { makeTraefikSchema } from '@/utils/stack-addon-schemas'

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

// Dropdown option lists fed to the editor. Badged with provenance
// ("docker" = live inspect, "host" = curated in the Host edit page).
function buildOpts(
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

const schema = computed(() =>
  makeTraefikSchema(
    () => buildOpts(props.discovery?.entryPoints, props.hostRefs?.entryPoints),
    () => buildOpts(props.discovery?.certResolvers, props.hostRefs?.certResolvers),
    () => buildOpts(props.discovery?.middlewares, props.hostRefs?.middlewares),
    {
      domain: props.hostRefs?.defaultDomain,
      entrypoint: props.hostRefs?.defaultEntrypoint,
      certResolver: props.hostRefs?.defaultCertResolver,
      middleware: props.hostRefs?.defaultMiddleware,
    },
  ),
)

const hostContext = computed(() => ({
  detected: props.discovery?.containerName || props.hostRefs?.containerName,
  detectedLabel: 'Traefik',
  stackName: props.hostRefs?.stackName,
  overrides: props.hostRefs?.overrides,
}))
</script>
