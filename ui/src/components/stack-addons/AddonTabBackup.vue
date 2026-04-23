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
import type { BackupServiceCfg } from '@/api/compose_stack'
import type { GenericAddonExtract } from '@/api/host'
import { makeBackupSchema } from '@/utils/stack-addon-schemas'

const props = defineProps<{
  services: string[]
  hostRefs: (GenericAddonExtract & { stackName?: string }) | null
  modelValue: Record<string, BackupServiceCfg>
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: Record<string, BackupServiceCfg>): void
}>()

const schema = computed(() =>
  makeBackupSchema({
    schedule: props.hostRefs?.defaults?.schedule,
    plugin: props.hostRefs?.defaults?.plugin,
  }),
)

const hostContext = computed(() => ({
  detected: props.hostRefs?.containerName,
  detectedLabel: 'Backup',
  stackName: props.hostRefs?.stackName,
  overrides: props.hostRefs?.overrides,
}))
</script>
