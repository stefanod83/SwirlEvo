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
import { computed } from 'vue'
import StructuredLabelsEditor from './StructuredLabelsEditor.vue'
import type { WatchtowerServiceCfg } from '@/api/compose_stack'
import type { GenericAddonExtract } from '@/api/host'
import { makeWatchtowerSchema } from '@/utils/stack-addon-schemas'

const props = defineProps<{
  services: string[]
  hostRefs: (GenericAddonExtract & { stackName?: string }) | null
  modelValue: Record<string, WatchtowerServiceCfg>
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: Record<string, WatchtowerServiceCfg>): void
}>()

const schema = computed(() =>
  makeWatchtowerSchema({
    monitorOnly: props.hostRefs?.defaults?.['monitor-only'] === 'true',
    scope: props.hostRefs?.defaults?.scope,
  }),
)

const hostContext = computed(() => ({
  detected: props.hostRefs?.containerName,
  detectedLabel: 'Watchtower',
  stackName: props.hostRefs?.stackName,
  overrides: props.hostRefs?.overrides,
}))
</script>
