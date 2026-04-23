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
import type { SablierServiceCfg } from '@/api/compose_stack'
import type { GenericAddonExtract } from '@/api/host'
import { makeSablierSchema } from '@/utils/stack-addon-schemas'

const props = defineProps<{
  services: string[]
  hostRefs: (GenericAddonExtract & { stackName?: string }) | null
  modelValue: Record<string, SablierServiceCfg>
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: Record<string, SablierServiceCfg>): void
}>()

const schema = computed(() =>
  makeSablierSchema({
    sessionDuration: props.hostRefs?.defaults?.session_duration,
    strategy: props.hostRefs?.defaults?.strategy,
  }),
)

const hostContext = computed(() => ({
  detected: props.hostRefs?.containerName,
  detectedLabel: 'Sablier',
  stackName: props.hostRefs?.stackName,
  overrides: props.hostRefs?.overrides,
}))
</script>
