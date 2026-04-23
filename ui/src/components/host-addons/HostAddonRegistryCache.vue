<template>
  <x-panel
    :title="t('host_addon_registry_cache.title')"
    :subtitle="t('host_addon_registry_cache.subtitle')"
    divider="bottom"
    :collapsed="effectiveCollapsed"
  >
    <template #action>
      <n-space :size="12" align="center">
        <!-- Collapse toggle — only rendered when parent controls the
             panel (opt-in via the `collapsed` prop). Mirrors the
             same pattern used by HostAddonTraefik. -->
        <n-button
          v-if="isControlled"
          secondary
          strong
          size="small"
          style="min-width: 75px"
          @click="() => emit('toggle')"
        >{{ effectiveCollapsed ? t('buttons.expand') : t('buttons.collapse') }}</n-button>
        <!-- Master enable switch: when on, the deploy-time image
             rewriter (Phase 3) will rewrite compose image: refs for
             stacks targeting this host. When off, pulls fall back to
             their authored upstream. -->
        <n-space :size="6" align="center">
          <span style="font-size: 12px">{{ t('host_addon_registry_cache.enabled') }}</span>
          <n-switch
            :value="!!local.enabled"
            :disabled="!status?.mirrorEnabled"
            @update:value="setEnabled"
          />
        </n-space>
      </n-space>
    </template>

    <!-- Mirror not configured yet → hard banner, nothing else is
         actionable. The global settings link saves the operator a
         click when the feature is still brand-new. -->
    <n-alert
      v-if="!loading && !status?.mirrorEnabled"
      type="warning"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ t('host_addon_registry_cache.mirror_disabled') }}
    </n-alert>

    <!-- Federation variant: swarm_via_swirl hosts cannot have their
         daemon.json edited by this portal (direct socket access is
         out of scope by design). Instead, the portal PUSHES the
         local Setting.RegistryCache to the peer Swirl running
         MODE=swarm, which then runs its own bootstrap internally.
         Keeps the full standalone / SSH path untouched. -->
    <template v-if="isFederation && status?.mirrorEnabled">
      <n-alert type="info" :show-icon="true" style="margin-bottom: 12px">
        {{ t('host_addon_registry_cache.federation_banner') }}
      </n-alert>
      <n-descriptions
        :column="1"
        size="small"
        label-placement="left"
        bordered
        style="margin-bottom: 12px"
      >
        <n-descriptions-item :label="t('host_addon_registry_cache.mirror_url')">
          <code style="font-size: 12px">{{ mirrorURL }}</code>
        </n-descriptions-item>
        <n-descriptions-item
          v-if="status?.mirrorFingerprint"
          :label="t('host_addon_registry_cache.fingerprint')"
        >
          <code style="font-size: 11px; word-break: break-all">{{ status.mirrorFingerprint }}</code>
        </n-descriptions-item>
      </n-descriptions>
      <div class="rc-block">
        <div class="rc-block-title">{{ t('host_addon_registry_cache.federation_sync_title') }}</div>
        <n-space v-if="status?.lastSyncAt" :size="8" align="center" style="margin-bottom: 8px">
          <n-tag size="small" :type="syncFingerprintMatches ? 'success' : 'warning'" round>
            {{ syncFingerprintMatches ? t('host_addon_registry_cache.applied_ok') : t('host_addon_registry_cache.applied_stale') }}
          </n-tag>
          <span class="sd-muted">
            {{ t('host_addon_registry_cache.applied_meta', {
              date: formatDate(status.lastSyncAt),
              who: status.lastSyncBy || '—',
            }) }}
          </span>
        </n-space>
        <n-space v-else style="margin-bottom: 8px">
          <span class="sd-muted">{{ t('host_addon_registry_cache.federation_sync_pending') }}</span>
        </n-space>
        <n-space>
          <n-button type="primary" size="small" :loading="syncing" @click="syncToPeer">
            {{ t('host_addon_registry_cache.federation_sync_btn') }}
          </n-button>
        </n-space>
      </div>
    </template>

    <!-- Everything below is the standalone/SSH bootstrap path. Only
         shown for non-federation hosts. -->
    <template v-if="!isFederation && status?.mirrorEnabled">
      <n-alert v-if="!local.enabled" type="info" :show-icon="true" style="margin-bottom: 12px">
        {{ t('host_addon_registry_cache.disabled_hint') }}
      </n-alert>

      <!-- Summary strip: mirror URL + fingerprint. Read-only, mirrors
           the value the Settings page owns. -->
      <n-descriptions
        :column="1"
        size="small"
        label-placement="left"
        bordered
        style="margin-bottom: 12px"
      >
        <n-descriptions-item :label="t('host_addon_registry_cache.mirror_url')">
          <code style="font-size: 12px">{{ mirrorURL }}</code>
        </n-descriptions-item>
        <n-descriptions-item
          v-if="status?.mirrorFingerprint"
          :label="t('host_addon_registry_cache.fingerprint')"
        >
          <code style="font-size: 11px; word-break: break-all">{{ status.mirrorFingerprint }}</code>
        </n-descriptions-item>
      </n-descriptions>

      <!-- Insecure mode toggle: switches between cert-distribution and
           insecure-registries bootstrap. Flipping it regenerates the
           snippet + script live on the next reload. -->
      <n-form label-placement="left" label-width="auto" :show-feedback="false">
        <n-form-item :label="t('host_addon_registry_cache.insecure_mode')" label-align="right">
          <n-switch v-model:value="local.insecureMode" />
        </n-form-item>
      </n-form>
      <div class="sd-hint">{{ t('host_addon_registry_cache.insecure_hint') }}</div>

      <!-- Bootstrap instructions: two boxes (script + snippet) with
           copy/download buttons. The script is the "paste on host"
           one-shot; the snippet is for operators who prefer manual
           jq merge. -->
      <div class="rc-block" style="margin-top: 16px">
        <div class="rc-block-title">{{ t('host_addon_registry_cache.bootstrap_script') }}</div>
        <n-input
          type="textarea"
          :value="status?.bootstrapScript || ''"
          readonly
          :autosize="{ minRows: 8, maxRows: 16 }"
          style="font-family: monospace; font-size: 11px"
        />
        <n-space style="margin-top: 6px">
          <n-button size="tiny" @click="copyText(status?.bootstrapScript || '')">
            {{ t('buttons.copy') }}
          </n-button>
          <n-button
            size="tiny"
            @click="downloadText(status?.bootstrapScript || '', 'swirl-registry-cache-bootstrap.sh')"
          >
            {{ t('buttons.download') }}
          </n-button>
        </n-space>
      </div>

      <div class="rc-block" style="margin-top: 16px">
        <div class="rc-block-title">{{ t('host_addon_registry_cache.daemon_snippet') }}</div>
        <n-input
          type="textarea"
          :value="status?.daemonSnippet || ''"
          readonly
          :autosize="{ minRows: 4, maxRows: 8 }"
          style="font-family: monospace; font-size: 11px"
        />
        <n-space style="margin-top: 6px">
          <n-button size="tiny" @click="copyText(status?.daemonSnippet || '')">
            {{ t('buttons.copy') }}
          </n-button>
        </n-space>
      </div>

      <!-- Applied-attestation area: single "Mark as applied" button
           + readback with fingerprint drift badge once saved. Swirl
           never touches the remote daemon.json itself, so the flag
           is a manual handshake. -->
      <div class="rc-block" style="margin-top: 16px">
        <div class="rc-block-title">{{ t('host_addon_registry_cache.applied_title') }}</div>
        <n-space v-if="status?.appliedAt" :size="8" align="center" style="margin-bottom: 8px">
          <n-tag size="small" :type="fingerprintMatches ? 'success' : 'warning'" round>
            {{ fingerprintMatches ? t('host_addon_registry_cache.applied_ok') : t('host_addon_registry_cache.applied_stale') }}
          </n-tag>
          <span class="sd-muted">
            {{ t('host_addon_registry_cache.applied_meta', {
              date: formatDate(status.appliedAt),
              who: status.appliedBy || '—',
            }) }}
          </span>
        </n-space>
        <n-space v-else style="margin-bottom: 8px">
          <span class="sd-muted">{{ t('host_addon_registry_cache.applied_pending') }}</span>
        </n-space>
        <n-button size="small" secondary @click="() => save(true)">
          {{ t('host_addon_registry_cache.mark_applied') }}
        </n-button>
      </div>

      <!-- Save strip: persist enabled + insecureMode without touching
           the applied attestation. -->
      <n-space style="margin-top: 16px">
        <n-button type="primary" :loading="saving" @click="() => save(false)">
          {{ t('buttons.save') }}
        </n-button>
        <n-popconfirm
          :show-icon="false"
          @positive-click="clear"
        >
          <template #trigger>
            <n-button size="small" quaternary type="error" :disabled="!local.enabled && !status?.appliedAt">
              {{ t('buttons.delete') }}
            </n-button>
          </template>
          {{ t('host_addon_registry_cache.clear_confirm') }}
        </n-popconfirm>
      </n-space>
    </template>
  </x-panel>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  NSpace, NButton, NAlert, NForm, NFormItem, NInput, NSwitch,
  NDescriptions, NDescriptionsItem, NTag, NPopconfirm, useMessage,
} from 'naive-ui'
import { useI18n } from 'vue-i18n'
import XPanel from '@/components/Panel.vue'
import {
  registryCacheGet, registryCacheSave, registryCacheSyncToPeer, clearAddonExtract,
  type RegistryCacheHostStatus,
} from '@/api/host'

const props = defineProps<{
  hostId: string
  // Host.Type — lets the component decide between the
  // standalone/SSH bootstrap path ("standalone") and the federation
  // delegation variant ("swarm_via_swirl"). The standalone path
  // generates daemon.json snippets + bootstrap script; the federation
  // path pushes Setting.RegistryCache to the peer Swirl.
  hostType?: string
  // Optional parent-controlled collapse (opt-in). Matches the pattern
  // used by HostAddonTraefik so the Host edit page can run a
  // Settings-style single-expanded accordion without every addon
  // owning its own state.
  collapsed?: boolean
}>()
const emit = defineEmits<{
  (e: 'toggle'): void
}>()
const { t } = useI18n()
const message = useMessage()

const isControlled = computed(() => props.collapsed !== undefined)
const effectiveCollapsed = computed(() => isControlled.value ? !!props.collapsed : false)
const isFederation = computed(() => props.hostType === 'swarm_via_swirl')

const loading = ref(false)
const saving = ref(false)
const syncing = ref(false)
const status = ref<RegistryCacheHostStatus | null>(null)
const local = reactive({
  enabled: false,
  insecureMode: false,
})

const mirrorURL = computed(() => {
  if (!status.value?.mirrorHostname) return '—'
  const port = status.value.mirrorPort ?? 5000
  return `https://${status.value.mirrorHostname}:${port}`
})

// Fingerprint match: when the operator previously attested a value
// different from the current live fingerprint, the host's daemon is
// still trusting an old CA — flag it so they re-apply the bootstrap.
const fingerprintMatches = computed(() => {
  if (!status.value?.appliedFingerprint) return true
  if (!status.value?.mirrorFingerprint) return true
  return status.value.appliedFingerprint === status.value.mirrorFingerprint
})

// Federation fingerprint drift: the portal stamps LastSyncFingerprint
// at the moment of a successful push; a subsequent CA rotation on the
// portal leaves the peer behind until the next sync.
const syncFingerprintMatches = computed(() => {
  if (!status.value?.lastSyncFingerprint) return true
  if (!status.value?.mirrorFingerprint) return true
  return status.value.lastSyncFingerprint === status.value.mirrorFingerprint
})

function formatDate(s: string): string {
  if (!s) return ''
  try { return new Date(s).toLocaleString() } catch { return s }
}

function setEnabled(v: boolean) {
  local.enabled = v
  // Auto-save on toggle so the operator does not have to also click
  // Save — matches the UX of the Traefik addon's master switch.
  save(false)
}

async function refresh() {
  if (!props.hostId) return
  loading.value = true
  try {
    const r = await registryCacheGet(props.hostId)
    status.value = (r.data ?? null) as RegistryCacheHostStatus | null
    if (status.value) {
      local.enabled = !!status.value.enabled
      local.insecureMode = !!status.value.insecureMode
    }
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    loading.value = false
  }
}

async function save(markApplied: boolean) {
  if (!props.hostId) return
  saving.value = true
  try {
    await registryCacheSave({
      hostId: props.hostId,
      enabled: local.enabled,
      insecureMode: local.insecureMode,
      markApplied,
    })
    await refresh()
    message.success(t('host_addon_registry_cache.save_ok'))
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    saving.value = false
  }
}

async function syncToPeer() {
  if (!props.hostId) return
  syncing.value = true
  try {
    await registryCacheSyncToPeer(props.hostId)
    await refresh()
    message.success(t('host_addon_registry_cache.federation_sync_ok'))
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    syncing.value = false
  }
}

async function clear() {
  if (!props.hostId) return
  try {
    await clearAddonExtract(props.hostId, 'registryCache')
    await refresh()
    message.success(t('host_addon_registry_cache.clear_ok'))
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  }
}

function copyText(s: string) {
  if (!s) return
  try {
    navigator.clipboard.writeText(s)
    message.success(t('texts.action_success'))
  } catch {
    message.error('copy failed')
  }
}

function downloadText(s: string, filename: string) {
  if (!s) return
  const blob = new Blob([s], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

watch(() => props.hostId, () => { refresh() })
onMounted(() => { refresh() })
</script>

<style scoped>
.rc-block {
  padding: 10px 12px;
  border: 1px solid var(--n-border-color, rgba(128, 128, 128, 0.2));
  border-radius: 4px;
  background-color: rgba(128, 128, 128, 0.04);
}
.rc-block-title {
  font-weight: 600;
  font-size: 13px;
  margin-bottom: 6px;
}
.sd-hint {
  font-size: 12px;
  color: var(--n-text-color-3, #666);
  margin: -6px 0 4px 6px;
  line-height: 1.45;
}
.sd-muted {
  color: var(--n-text-color-3, #888);
  font-size: 12px;
}
</style>
