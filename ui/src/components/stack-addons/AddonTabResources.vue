<template>
  <n-space vertical :size="12">
    <n-alert :type="mode === 'swarm' ? 'info' : 'default'" :show-icon="true" style="padding: 8px 12px">
      {{ mode === 'swarm'
        ? t('stack_addon_resources.mode_swarm')
        : t('stack_addon_resources.mode_standalone') }}
    </n-alert>

    <n-alert v-if="!services.length" type="info" :show-icon="true">
      {{ t('stack_addon_resources.no_services') }}
    </n-alert>

    <n-table v-else size="small" :bordered="true" :single-line="false">
      <thead>
        <tr>
          <th>{{ t('objects.service') }}</th>
          <th>{{ t('stack_addon_resources.cpus_limit') }}</th>
          <th>{{ t('stack_addon_resources.memory_limit') }}</th>
          <th>{{ t('stack_addon_resources.cpus_reservation') }}</th>
          <th>{{ t('stack_addon_resources.memory_reservation') }}</th>
          <th style="width: 60px"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="svc of services" :key="svc">
          <td><code>{{ svc }}</code></td>
          <td>
            <n-input
              size="small"
              :value="row(svc).cpusLimit || ''"
              :placeholder="cpuPlaceholder"
              @update:value="set(svc, { cpusLimit: $event })"
            />
          </td>
          <td>
            <n-input
              size="small"
              :value="row(svc).memoryLimit || ''"
              :placeholder="memPlaceholder"
              :status="memoryInputStatus(row(svc).memoryLimit)"
              @update:value="set(svc, { memoryLimit: $event })"
            />
          </td>
          <td>
            <n-input
              size="small"
              :value="row(svc).cpusReservation || ''"
              :placeholder="cpuPlaceholder"
              @update:value="set(svc, { cpusReservation: $event })"
            />
          </td>
          <td>
            <n-input
              size="small"
              :value="row(svc).memoryReservation || ''"
              :placeholder="memPlaceholder"
              :status="memoryInputStatus(row(svc).memoryReservation)"
              @update:value="set(svc, { memoryReservation: $event })"
            />
          </td>
          <td>
            <n-button
              v-if="anySet(svc)"
              size="tiny"
              quaternary
              type="error"
              @click="reset(svc)"
            >{{ t('buttons.delete') }}</n-button>
          </td>
        </tr>
      </tbody>
    </n-table>

    <!-- Summary of validation hints: mem values must match \d+[kKmMgG]? -->
    <n-alert v-if="memoryFormatHint" type="warning" :show-icon="true">
      {{ t('stack_addon_resources.memory_hint') }}
    </n-alert>
  </n-space>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NSpace, NAlert, NTable, NInput, NButton } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import type { ResourcesServiceCfg } from '@/api/compose_stack'

const props = defineProps<{
  services: string[]
  mode: 'swarm' | 'standalone'
  modelValue: Record<string, ResourcesServiceCfg>
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: Record<string, ResourcesServiceCfg>): void
}>()

const { t } = useI18n()

const cpuPlaceholder = computed(() => t('stack_addon_resources.cpus_placeholder'))
const memPlaceholder = computed(() => t('stack_addon_resources.memory_placeholder'))

function row(svc: string): ResourcesServiceCfg {
  return props.modelValue[svc] || {}
}

function set(svc: string, patch: Partial<ResourcesServiceCfg>) {
  const next = { ...props.modelValue }
  next[svc] = { ...(next[svc] || {}), ...patch }
  // Strip the service entry entirely when every field is empty — keeps
  // the JSON payload compact and avoids emitting an unused placeholder.
  if (!anySetOn(next[svc])) {
    delete next[svc]
  }
  emit('update:modelValue', next)
}

function anySet(svc: string): boolean {
  return anySetOn(props.modelValue[svc])
}

function anySetOn(r: ResourcesServiceCfg | undefined): boolean {
  if (!r) return false
  return !!(r.cpusLimit || r.cpusReservation || r.memoryLimit || r.memoryReservation)
}

function reset(svc: string) {
  const next = { ...props.modelValue }
  delete next[svc]
  emit('update:modelValue', next)
}

// Memory values should match compose's size suffixes; we flag but don't
// block — the backend passes them through verbatim and docker parses
// the final form.
const memoryRegex = /^\s*\d+(\.\d+)?\s*[kKmMgGtT]?[bB]?\s*$/

function memoryInputStatus(v: string | undefined): 'success' | 'warning' | undefined {
  if (!v) return undefined
  return memoryRegex.test(v) ? 'success' : 'warning'
}

const memoryFormatHint = computed(() => {
  for (const svc of Object.keys(props.modelValue)) {
    const r = props.modelValue[svc]
    for (const v of [r.memoryLimit, r.memoryReservation]) {
      if (v && !memoryRegex.test(v)) return true
    }
  }
  return false
})
</script>
