<template>
  <div>
    <n-alert v-if="!loading && !preview?.mirrorEnabled" type="warning" :show-icon="true" style="margin-bottom: 12px">
      {{ t('stack_addon_registry_cache.mirror_disabled') }}
    </n-alert>
    <n-alert
      v-else-if="!loading && preview?.effectivelyDisabled"
      type="info"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ t('stack_addon_registry_cache.effectively_disabled') }}
    </n-alert>

    <!-- Per-stack opt-out. Even when the mirror is enabled globally + on
         the host, operators may pin a stack to the upstream (e.g. the
         registry:2 itself, CI runners). Saved with the stack. -->
    <n-space :size="8" align="center" style="margin-bottom: 12px">
      <n-checkbox :checked="disabled" @update:checked="onToggleDisable">
        {{ t('stack_addon_registry_cache.disable_for_stack') }}
      </n-checkbox>
      <span class="hint">{{ t('stack_addon_registry_cache.disable_hint') }}</span>
    </n-space>

    <!-- Pre-warm: pre-pulls every rewritten image through the local
         Swirl daemon. Populates the mirror's cache before the first
         target-host deploy so the heavy layer traffic hits the faster
         portal-to-mirror link. Enabled only when we have rewrite
         actions (no point pulling nothing). -->
    <n-space
      v-if="hasRewrites"
      :size="8"
      align="center"
      style="margin-bottom: 12px"
    >
      <n-button size="small" :loading="warming" @click="prewarm">
        {{ t('stack_addon_registry_cache.prewarm_btn') }}
      </n-button>
      <span class="hint">{{ t('stack_addon_registry_cache.prewarm_hint') }}</span>
    </n-space>
    <n-alert
      v-if="warmSummary"
      :type="warmSummaryType"
      :show-icon="true"
      closable
      style="margin-bottom: 12px"
      @close="warmSummary = null"
    >
      {{ warmSummary }}
    </n-alert>

    <!-- Preview table: one row per service. Rewrites show as
         original → rewritten with the matched upstream/prefix; skips
         show the reason. -->
    <n-data-table
      v-if="preview?.actions?.length"
      :columns="columns"
      :data="preview.actions"
      :row-key="(a: RegistryCacheRewriteAction) => a.service"
      size="small"
      :bordered="true"
    />
    <div v-else class="sd-muted" style="padding: 8px 4px; font-size: 13px">
      {{ t('stack_addon_registry_cache.empty_hint') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, h, ref, watch } from 'vue'
import { NAlert, NCheckbox, NDataTable, NSpace, NTag } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import composeStackApi, {
  type RegistryCachePreviewResponse,
  type RegistryCacheRewriteAction,
} from '@/api/compose_stack'

const props = defineProps<{
  hostId: string
  content: string
  disabled: boolean
}>()
const emit = defineEmits<{
  (e: 'update:disabled', v: boolean): void
}>()

const { t } = useI18n()

const loading = ref(false)
const preview = ref<RegistryCachePreviewResponse | null>(null)
const warming = ref(false)
const warmSummary = ref<string | null>(null)
const warmSummaryType = ref<'success' | 'warning' | 'error'>('success')

// hasRewrites reflects whether at least one preview action has a
// non-empty Rewritten target. Used to hide the pre-warm button when
// there is literally nothing to pull (empty stack / all digest-pinned /
// rewriter disabled).
const hasRewrites = computed(() => {
  if (!preview.value?.actions) return false
  return preview.value.actions.some(a => !!a.rewritten)
})

async function prewarm() {
  if (!props.hostId || !props.content) return
  warming.value = true
  warmSummary.value = null
  try {
    const r = await composeStackApi.registryCacheWarm({
      hostId: props.hostId,
      content: props.content,
      disableRegistryCache: props.disabled,
    })
    const results = r.data?.results || []
    const ok = results.filter((x: any) => x.ok).length
    const fail = results.length - ok
    if (fail === 0) {
      warmSummaryType.value = 'success'
      warmSummary.value = t('stack_addon_registry_cache.prewarm_ok', { count: ok })
    } else if (ok === 0) {
      warmSummaryType.value = 'error'
      warmSummary.value = t('stack_addon_registry_cache.prewarm_fail', { count: fail })
    } else {
      warmSummaryType.value = 'warning'
      warmSummary.value = t('stack_addon_registry_cache.prewarm_partial', { ok, fail })
    }
  } catch (e: any) {
    warmSummaryType.value = 'error'
    warmSummary.value = e?.response?.data?.info || e?.message || String(e)
  } finally {
    warming.value = false
  }
}

function onToggleDisable(v: boolean) {
  emit('update:disabled', v)
}

const columns = computed(() => [
  {
    title: t('stack_addon_registry_cache.col_service'),
    key: 'service',
    width: 140,
    render: (r: RegistryCacheRewriteAction) => h('code', { style: 'font-size: 12px' }, r.service),
  },
  {
    title: t('stack_addon_registry_cache.col_original'),
    key: 'original',
    render: (r: RegistryCacheRewriteAction) =>
      h('code', { style: 'font-size: 11px; word-break: break-all' }, r.original),
  },
  {
    title: t('stack_addon_registry_cache.col_rewritten'),
    key: 'rewritten',
    render: (r: RegistryCacheRewriteAction) => {
      if (r.rewritten) {
        return h('code', { style: 'font-size: 11px; word-break: break-all; color: var(--n-primary-color, #36ad6a)' }, r.rewritten)
      }
      const reason = r.reason || 'no-match'
      const labelKey = `stack_addon_registry_cache.reason_${reason.replace('-', '_')}`
      const label = t(labelKey)
      // Tag type mapping: no-match → default (expected upstream not mapped),
      // digest-preserved → info (intentional skip), invalid-ref → error.
      let type: 'default' | 'info' | 'warning' | 'error' = 'default'
      if (reason === 'digest-preserved') type = 'info'
      else if (reason === 'invalid-ref') type = 'error'
      return h(NTag, { size: 'small', type, round: true }, { default: () => label === labelKey ? reason : label })
    },
  },
  {
    title: t('stack_addon_registry_cache.col_upstream'),
    key: 'upstream',
    width: 140,
    render: (r: RegistryCacheRewriteAction) => {
      if (!r.upstream) return h('span', { class: 'sd-muted' }, '—')
      return h('code', { style: 'font-size: 11px' }, r.upstream)
    },
  },
])

let debounceTimer: number | null = null

async function refresh() {
  if (!props.hostId || !props.content) {
    preview.value = null
    return
  }
  loading.value = true
  try {
    const r = await composeStackApi.registryCachePreview({
      hostId: props.hostId,
      content: props.content,
      disableRegistryCache: props.disabled,
    })
    preview.value = (r.data ?? null) as RegistryCachePreviewResponse | null
  } catch (e: any) {
    window.message?.error?.(e?.response?.data?.info || e?.message || String(e))
    preview.value = null
  } finally {
    loading.value = false
  }
}

function scheduleRefresh() {
  if (debounceTimer !== null) window.clearTimeout(debounceTimer)
  debounceTimer = window.setTimeout(() => { refresh() }, 300)
}

// Re-preview on content / host / toggle changes. Debounced to avoid
// thrashing the backend while the operator types in the YAML editor.
watch(() => [props.hostId, props.content, props.disabled], scheduleRefresh, { immediate: true })
</script>

<style scoped>
.hint {
  font-size: 12px;
  color: var(--n-text-color-3, #888);
}
.sd-muted {
  color: var(--n-text-color-3, #888);
}
</style>
