<template>
  <!--
    Small shared badge used by the Vault Secret pages (List row + Edit
    status panel) to show the current KVv2 version or an error state.
    Never renders anything for `loading` state to keep list rows stable.
  -->
  <n-tooltip v-if="state" trigger="hover">
    <template #trigger>
      <n-tag size="small" :type="tagType" round>
        {{ label }}
      </n-tag>
    </template>
    {{ tooltip }}
  </n-tooltip>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NTag, NTooltip } from 'naive-ui'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  state: 'ok' | 'missing' | 'error' | ''
  currentVersion?: number
  totalVersions?: number
  error?: string
}>()

const { t } = useI18n()

const tagType = computed(() => {
  switch (props.state) {
    case 'ok': return 'info'
    case 'missing': return 'error'
    case 'error': return 'warning'
    default: return 'default'
  }
})

const label = computed(() => {
  switch (props.state) {
    case 'ok': return `v${props.currentVersion ?? 0}`
    case 'missing': return t('vault_secret.missing')
    case 'error': return t('vault_secret.status_error')
    default: return ''
  }
})

const tooltip = computed(() => {
  switch (props.state) {
    case 'ok':
      return t('vault_secret.status_tooltip_ok', { total: props.totalVersions ?? 0 })
    case 'missing':
      return t('vault_secret.status_tooltip_missing')
    case 'error':
      return props.error || t('vault_secret.status_tooltip_error')
    default:
      return ''
  }
})
</script>
