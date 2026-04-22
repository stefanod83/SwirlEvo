<template>
  <div v-if="hasContent" class="label-preview">
    <div class="header">
      <n-icon :component="EyeIcon" />
      <span>{{ t('stack_addon_common.preview_title') }}</span>
      <span class="muted">({{ t('stack_addon_common.preview_hint') }})</span>
    </div>
    <pre class="body">{{ rendered }}</pre>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NIcon } from 'naive-ui'
import { EyeOutline as EyeIcon } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  labelsByService: Record<string, Record<string, string>>
}>()

const { t } = useI18n()

const hasContent = computed(() => {
  for (const s of Object.keys(props.labelsByService)) {
    if (Object.keys(props.labelsByService[s] || {}).length > 0) return true
  }
  return false
})

const rendered = computed(() => {
  // Render in a compose-flavored shape so the operator can visually map
  // each line to the YAML it will produce. Not valid YAML on its own
  // because the field only shows the managed subset.
  const lines: string[] = ['services:']
  for (const svc of Object.keys(props.labelsByService).sort()) {
    const kv = props.labelsByService[svc] || {}
    if (!Object.keys(kv).length) continue
    lines.push(`  ${svc}:`)
    lines.push(`    labels:`)
    for (const k of Object.keys(kv).sort()) {
      const v = kv[k]
      lines.push(`      ${k}: ${formatValue(v)}  # swirl-managed`)
    }
  }
  return lines.join('\n')
})

function formatValue(v: string): string {
  // Quote values with special chars so the preview hints at the actual
  // YAML output (the Go emitter also quotes these).
  if (/^[0-9]+$/.test(v) || v === 'true' || v === 'false') return `"${v}"`
  if (/[:`#]/.test(v)) return v
  return v
}
</script>

<style scoped>
.label-preview {
  margin-top: 12px;
  border-radius: 4px;
  background: var(--n-code-color, rgba(128, 128, 128, 0.08));
  padding: 10px 12px;
}
.header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  opacity: 0.75;
  margin-bottom: 6px;
}
.muted {
  font-weight: 400;
  opacity: 0.6;
  text-transform: none;
  letter-spacing: 0;
}
.body {
  margin: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
