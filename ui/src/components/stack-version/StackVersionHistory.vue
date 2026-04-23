<template>
  <div class="stack-version-history">
    <n-space align="center" :size="8">
      <n-select
        size="small"
        :options="options"
        :value="null"
        :placeholder="
          loading
            ? t('stack_version.loading')
            : versions.length
              ? t('stack_version.pick_hint')
              : t('stack_version.empty')
        "
        :disabled="!versions.length || loading"
        style="min-width: 360px"
        @update:value="openVersion"
      />
      <n-button size="small" quaternary :loading="loading" @click="reload">
        <template #icon>
          <n-icon><refresh-icon /></n-icon>
        </template>
        {{ t('buttons.refresh') }}
      </n-button>
    </n-space>

    <!-- Diff modal: side-by-side Content comparison, with Restore action. -->
    <n-modal
      v-model:show="modalOpen"
      preset="card"
      :title="modalTitle"
      style="width: 90vw; max-width: 1200px"
      :mask-closable="false"
    >
      <n-spin :show="modalLoading">
        <n-space vertical :size="12">
          <n-space v-if="selected" :size="12" align="center">
            <n-tag type="info" size="small">rev {{ selected.revision }}</n-tag>
            <n-tag size="small">{{ selected.reason }}</n-tag>
            <span v-if="selected.createdAt" class="muted">
              {{ formatTimestamp(selected.createdAt) }}
            </span>
            <span v-if="selected.createdBy?.name" class="muted">
              · {{ selected.createdBy.name }}
            </span>
          </n-space>

          <n-grid cols="2" x-gap="12">
            <n-gi>
              <div class="col-header">{{ t('stack_version.previous') }}</div>
              <pre class="yaml-pre">{{ selected?.content || '' }}</pre>
            </n-gi>
            <n-gi>
              <div class="col-header">{{ t('stack_version.current') }}</div>
              <pre class="yaml-pre">{{ currentContent }}</pre>
            </n-gi>
          </n-grid>
        </n-space>
      </n-spin>

      <template #footer>
        <n-space justify="end">
          <n-button @click="modalOpen = false">{{ t('buttons.cancel') }}</n-button>
          <n-popconfirm :show-icon="false" @positive-click="restore">
            <template #trigger>
              <n-button
                type="warning"
                :disabled="!selected || restoring"
                :loading="restoring"
              >
                {{ t('stack_version.restore') }}
              </n-button>
            </template>
            {{ t('stack_version.restore_confirm') }}
          </n-popconfirm>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NSpace, NSelect, NButton, NModal, NSpin, NGrid, NGi, NTag, NPopconfirm,
  NIcon,
  useMessage,
} from 'naive-ui'
import { RefreshOutline as RefreshIcon } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import composeStackApi from '@/api/compose_stack'
import type { ComposeStackVersion } from '@/api/compose_stack'

const props = defineProps<{
  stackId: string
  currentContent: string
}>()
const emit = defineEmits<{
  (e: 'restored'): void
}>()

const { t } = useI18n()
const message = useMessage()

const versions = ref<ComposeStackVersion[]>([])
const loading = ref(false)
const modalOpen = ref(false)
const modalLoading = ref(false)
const selected = ref<ComposeStackVersion | null>(null)
const restoring = ref(false)

const options = computed(() =>
  versions.value.map(v => ({
    label: formatOption(v),
    value: v.id,
  }))
)

const modalTitle = computed(() =>
  selected.value
    ? `${t('stack_version.diff_title')} — rev ${selected.value.revision}`
    : t('stack_version.diff_title')
)

// formatTimestamp renders the backend-emitted RFC3339-ish timestamp as
// dd/mm/yyyy HH:mm:ss. Falls back to the raw string when parsing fails.
function formatTimestamp(ts?: string): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ts
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)}/${d.getFullYear()} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function formatOption(v: ComposeStackVersion): string {
  const parts: string[] = [`rev ${v.revision}`]
  const ts = formatTimestamp(v.createdAt)
  if (ts) parts.push(ts)
  if (v.createdBy?.name) parts.push(v.createdBy.name)
  if (v.reason) parts.push(v.reason)
  return parts.join(' · ')
}

async function reload() {
  if (!props.stackId) {
    versions.value = []
    return
  }
  loading.value = true
  try {
    const r = await composeStackApi.versions(props.stackId)
    versions.value = (r.data as any)?.items || []
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    loading.value = false
  }
}

async function openVersion(versionId: string | null) {
  if (!versionId) return
  modalOpen.value = true
  modalLoading.value = true
  selected.value = null
  try {
    const r = await composeStackApi.versionGet(versionId)
    selected.value = (r.data as ComposeStackVersion) || null
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
    modalOpen.value = false
  } finally {
    modalLoading.value = false
  }
}

async function restore() {
  if (!selected.value || !props.stackId) return
  restoring.value = true
  try {
    await composeStackApi.versionRestore(props.stackId, selected.value.id)
    message.success(t('stack_version.restored'))
    modalOpen.value = false
    emit('restored')
    await reload()
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    restoring.value = false
  }
}

watch(() => props.stackId, () => { reload() }, { immediate: true })

defineExpose({ reload })
</script>

<style scoped>
.stack-version-history {
  display: flex;
  align-items: center;
}
.muted {
  color: var(--n-text-color-3, #999);
  font-size: 12px;
}
.col-header {
  font-size: 12px;
  font-weight: 600;
  opacity: 0.7;
  margin-bottom: 6px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.yaml-pre {
  margin: 0;
  padding: 12px;
  background: var(--n-code-color, rgba(128, 128, 128, 0.08));
  border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 60vh;
  overflow: auto;
}
</style>
