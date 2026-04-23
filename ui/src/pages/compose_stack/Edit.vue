<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="onReturn">
        <template #icon>
          <n-icon>
            <arrow-back-icon />
          </n-icon>
        </template>
        {{ t('buttons.return') }}
      </n-button>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="16">
    <n-form :model="model" ref="form" :rules="rules" label-placement="top">
      <n-grid cols="2" x-gap="16">
        <n-form-item-gi :label="t('objects.host')" path="hostId">
          <n-select
            filterable
            :options="hosts"
            v-model:value="model.hostId"
            :disabled="isEdit"
            :placeholder="t('objects.host')"
          />
        </n-form-item-gi>
        <n-form-item-gi :label="t('fields.name')" path="name">
          <n-input v-model:value="model.name" :disabled="isEdit" :placeholder="t('fields.name')" />
        </n-form-item-gi>
      </n-grid>

      <!--
        Version history dropdown — visible only while editing an existing
        stack. Reveals prior snapshots captured before each content-changing
        save, with a side-by-side diff modal and a Restore action.
      -->
      <div v-if="isEdit" style="margin-bottom: 12px">
        <StackVersionHistory
          ref="historyRef"
          :stack-id="model.id || ''"
          :current-content="model.content"
          @restored="reloadStackFromServer"
        />
      </div>

      <!--
        Env file (.env-style variables) — rendered ABOVE the tab layout so
        operators can reference ${VAR} from every wizard tab (Traefik
        domain, Resources limits, etc.) without switching back to the
        compose tab. Persisted alongside the stack and substituted into
        the compose YAML at deploy time.
      -->
      <x-panel
        :title="t('stack_secret.env_file_title')"
        :subtitle="t('stack_secret.env_file_subtitle')"
        divider="bottom"
      >
        <n-input
          type="textarea"
          v-model:value="model.envFile"
          :placeholder="t('stack_secret.env_file_placeholder')"
          :autosize="{ minRows: 3, maxRows: 12 }"
          :input-props="{ style: 'font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px;' }"
        />
      </x-panel>

      <!--
        Addon wizard tab layout. `display-directive="show"` keeps every pane
        mounted so field state persists across tab switches.
      -->
      <n-tabs v-model:value="activeTab" type="line" size="large" display-directive="show">
        <n-tab-pane name="compose" :tab="'YAML'">
          <n-form-item :label="t('fields.content')" path="content" :show-label="false">
            <x-code-mirror
              v-model="model.content"
              :style="{ width: '100%', height: '55vh' }"
            />
          </n-form-item>
        </n-tab-pane>

        <n-tab-pane name="secrets" :tab="t('stack_secret.title')">
          <n-alert
            v-if="!isEdit"
            type="info"
            :show-icon="true"
          >
            {{ t('stack_secret.save_first_hint') }}
          </n-alert>
          <StackSecretsPanel
            v-else
            :stack-id="model.id || ''"
            :service-names="serviceNames"
          />
        </n-tab-pane>

        <!--
          Addon tabs are host-gated: each one appears only when the
          operator has explicitly flipped `enabled=true` on the host's
          AddonConfigExtract from the Host edit page. Keeps the editor
          focused on what's actually in play on the target host.
        -->
        <n-tab-pane v-if="traefikTabVisible" name="traefik" :tab="'Traefik'">
          <AddonTabTraefik
            :services="serviceNames"
            :discovery="hostAddons?.traefik || null"
            :host-refs="traefikHostRefs"
            :mode="hostMode"
            v-model="traefikCfgModel"
          />
        </n-tab-pane>

        <n-tab-pane name="resources" :tab="t('stack_addon_resources.title') || 'Resources'">
          <AddonTabResources
            :services="serviceNames"
            :mode="hostMode"
            v-model="resourcesCfgModel"
          />
        </n-tab-pane>

        <!-- Registry Cache preview: always visible (no host gate) so
             operators can see what a deploy would do even before any
             per-host bootstrap is applied. The tab itself explains
             why no rewrite is happening when the mirror is off / the
             host is not opted in. -->
        <n-tab-pane name="registry_cache" :tab="t('stack_addon_registry_cache.title')">
          <AddonTabRegistryCache
            :host-id="model.hostId"
            :content="model.content"
            :disabled="!!model.disableRegistryCache"
            @update:disabled="(v: boolean) => model.disableRegistryCache = v"
          />
        </n-tab-pane>
      </n-tabs>

      <n-space>
        <n-checkbox v-model:checked="pullImages">{{ t('fields.pull_images') || 'Pull images' }}</n-checkbox>
      </n-space>
    </n-form>

    <n-space>
      <n-button
        :type="isSelfDeployStack ? 'warning' : 'primary'"
        :loading="submitting"
        :disabled="!sdConfigLoaded"
        @click="isSelfDeployStack ? autoDeployStack() : deployStack()"
      >
        <template #icon>
          <n-icon><rocket-outline /></n-icon>
        </template>
        {{ isSelfDeployStack ? t('self_deploy.actions.auto_deploy') : t('buttons.deploy') }}
      </n-button>
      <n-button secondary :loading="submitting" @click="saveStack">
        <template #icon>
          <n-icon><save-outline /></n-icon>
        </template>
        {{ t('buttons.save') }}
      </n-button>
    </n-space>

    <!-- Auto-Deploy confirmation dialog -->
    <n-modal
      v-model:show="autoDeployConfirm"
      preset="dialog"
      type="warning"
      :title="t('self_deploy.actions.auto_deploy_confirm_title')"
      :positive-text="t('self_deploy.actions.auto_deploy')"
      :negative-text="t('buttons.cancel')"
      :positive-button-props="{ loading: submitting }"
      @positive-click="confirmAutoDeploy"
      @negative-click="autoDeployConfirm = false"
    >
      <div>{{ t('self_deploy.actions.auto_deploy_confirm_body') }}</div>
    </n-modal>

    <!-- Persistent Auto-Deploy error banner: populated when the
         /api/self-deploy/deploy call fails (e.g. preflight block
         because of an invalid stack YAML). Survives the toast fade
         so the operator can read multi-line backend messages. -->
    <n-alert
      v-if="autoDeployError"
      type="error"
      :show-icon="true"
      closable
      :title="t('self_deploy.errors.deploy_failed')"
      style="margin-top: 12px"
      @close="autoDeployError = ''"
    >
      <pre style="margin: 0; white-space: pre-wrap; font-size: 12px">{{ autoDeployError }}</pre>
    </n-alert>

    <!-- Deploy-in-progress modal (shared composable): spinner +
         status text, polls /api/system/mode. -->
    <n-modal
      v-model:show="progressOpen"
      :mask-closable="false"
      :closable="false"
      preset="card"
      :bordered="false"
      style="max-width: 520px;"
    >
      <template #header>
        <n-space align="center" :size="8">
          <n-spin size="small" />
          <span>{{ t('self_deploy.progress.title') }}</span>
        </n-space>
      </template>
      <template #header-extra>
        <n-tag v-if="progressTimedOut" type="warning" size="small" round>
          {{ t('self_deploy.progress.timeout') }}
        </n-tag>
      </template>
      <n-space vertical :size="12" style="padding: 8px 4px;">
        <div style="font-size: 14px; line-height: 1.5">
          {{ progressDescription }}
        </div>
        <div class="muted" style="font-size: 13px">
          {{ progressStatus }}
          <span v-if="progressElapsed" style="margin-left: 8px; opacity: 0.7">({{ progressElapsed }})</span>
        </div>
        <div v-if="currentJobId" class="muted" style="font-size: 12px">
          {{ t('self_deploy.status.job_id') }}: <code>{{ currentJobId }}</code>
        </div>
      </n-space>
    </n-modal>
  </n-space>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import {
  NSpace, NButton, NForm, NFormItem, NFormItemGi, NGrid, NInput,
  NSelect, NCheckbox, NIcon, NTag, NSpin,
  NModal, NAlert, NTabs, NTabPane,
  useMessage,
} from "naive-ui";
import {
  ArrowBackCircleOutline as ArrowBackIcon,
  RocketOutline, SaveOutline,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XPanel from "@/components/Panel.vue";
import XCodeMirror from "@/components/CodeMirror.vue";
import StackVersionHistory from "@/components/stack-version/StackVersionHistory.vue";
import AddonTabTraefik from "@/components/stack-addons/AddonTabTraefik.vue";
import AddonTabResources from "@/components/stack-addons/AddonTabResources.vue";
import AddonTabRegistryCache from "@/components/stack-addons/AddonTabRegistryCache.vue";
import composeStackApi from "@/api/compose_stack";
import type {
  ComposeStack, HostAddons, AddonsConfig,
  TraefikServiceCfg, ResourcesServiceCfg,
} from "@/api/compose_stack";
import { parseServiceNames } from "@/utils/stack-addon-parse";
import StackSecretsPanel from "@/components/stack-secret/StackSecretsPanel.vue";
import * as hostApi from "@/api/host";
import type { AddonConfigExtract } from "@/api/host";
import selfDeployApi, { type SelfDeployConfig } from "@/api/self-deploy";
import { useAutoDeployProgress } from "@/composables/useAutoDeployProgress";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'
import { requiredRule } from "@/utils/form";
import { returnTo } from "@/utils/nav";

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const message = useMessage()
const form = ref()
const submitting = ref(false)
const pullImages = ref(false)
const isEdit = computed(() => !!route.params.id)

function onReturn() {
  // When editing an existing stack and there's no history (deep link), go to
  // the stack's detail page rather than the list — the detail page is the
  // "parent" context for the editor.
  if (isEdit.value && route.params.id) {
    returnTo({ name: 'std_stack_detail', params: { id: route.params.id as string } })
  } else {
    returnTo({ name: 'std_stack_list' })
  }
}

const model = reactive({
  id: '',
  hostId: '',
  name: '',
  content: '',
  envFile: '',
  errorMessage: '',
  disableRegistryCache: false,
} as ComposeStack)

const hosts: any = ref([])
const rules = {
  hostId: requiredRule(),
  name: requiredRule(),
  content: requiredRule(),
}

// activeTab drives the addon wizard tab layout. Preserved across tab switches
// via display-directive="show" so editor state is never remounted.
const activeTab = ref<'compose' | 'traefik' | 'sablier' | 'watchtower' | 'backup' | 'resources' | 'registry_cache'>('compose')

// hostAddons caches the /compose-stack/host-addons response for the currently
// selected host. Re-fetched on hostId change. Null while loading or when the
// host has no detected addons — downstream tabs render generic defaults in
// that case.
const hostAddons = ref<HostAddons | null>(null)
const hostAddonsLoading = ref(false)

// hostAddonExtract mirrors Host.AddonConfigExtract for the currently
// selected host. Curated in the Host edit page (pointers to the stack
// that runs Traefik, uploaded lists, defaults, overrides); surfaced here
// as read-only context for the addon tabs.
const hostAddonExtract = ref<AddonConfigExtract | null>(null)
// stackNameById lets the addon tabs render the friendly name of the
// stack a host references instead of the opaque ID.
const stackNameById = ref<Record<string, string>>({})

// serviceNames is reactive: every YAML edit refreshes the list so the addon
// tabs see the current service set without manual refresh.
const serviceNames = computed(() => parseServiceNames(model.content))

async function loadHostAddons(hostId: string) {
  if (!hostId) {
    hostAddons.value = null
    hostAddonExtract.value = null
    return
  }
  hostAddonsLoading.value = true
  try {
    // Fire discovery + extract fetch in parallel. Discovery is the live
    // docker-inspect result; the extract is the operator-curated blob
    // saved from the Host edit page.
    const [discoverRes, extractRes] = await Promise.allSettled([
      composeStackApi.hostAddons(hostId),
      hostApi.getAddonExtract(hostId),
    ])
    if (discoverRes.status === 'fulfilled') {
      hostAddons.value = (discoverRes.value.data as HostAddons) || null
    } else {
      hostAddons.value = null
    }
    if (extractRes.status === 'fulfilled') {
      hostAddonExtract.value = (extractRes.value.data as AddonConfigExtract) || null
    } else {
      hostAddonExtract.value = null
    }
    // Build a {id: name} lookup for every stack on this host so the
    // Traefik tab can render "reference stack: swirl-traefik" instead
    // of "reference stack: 3f8a21d0".
    try {
      const r = await composeStackApi.search({ hostId, pageIndex: 1, pageSize: 1000 })
      const items = ((r.data as any)?.items || []) as { id: string; name: string }[]
      const map: Record<string, string> = {}
      for (const s of items) map[s.id] = s.name
      stackNameById.value = map
    } catch {
      stackNameById.value = {}
    }
  } finally {
    hostAddonsLoading.value = false
  }
}

watch(() => model.hostId, (hid) => { loadHostAddons(hid) })

// traefikHostRefs projects the Host.AddonConfigExtract.Traefik subtree into
// the shape AddonTabTraefik expects, resolving stackId → stackName so the
// tab can badge the stack that runs Traefik.
const traefikHostRefs = computed(() => {
  const t = hostAddonExtract.value?.traefik
  if (!t) return null
  return {
    ...t,
    stackName: t.stackId ? stackNameById.value[t.stackId] || t.stackId : undefined,
  }
})

// traefikTabVisible reflects the host-level master switch. The tab is
// only rendered when the operator has explicitly enabled the Traefik
// integration for the selected host from the Host edit page.
const traefikTabVisible = computed(() => !!hostAddonExtract.value?.traefik?.enabled)

// hostMode is inferred from the selected host entity and forwarded to the
// addon tabs — "swarm" means labels land under deploy.labels, otherwise
// top-level. Defaults to "standalone" while the host list loads.
const hostMode = computed<'swarm' | 'standalone'>(() => {
  const h: any = (hosts.value as any[])?.find((x: any) => x.value === model.hostId)
  return h?.type === 'swarm_via_swirl' ? 'swarm' : 'standalone'
})

// addonsConfig is the wizard state kept in-memory and sent alongside the
// compose payload on Save/Deploy. It's rebuilt from the persisted content
// via the server-side parser (POST /compose-stack/parse-addons) so the tabs
// never drift from what the Go emitter would produce on the next save.
const addonsConfig = reactive<AddonsConfig>({ traefik: {}, resources: {} })

async function reverseParseAddons(content: string) {
  if (!content) {
    addonsConfig.traefik = {}
    addonsConfig.resources = {}
    return
  }
  try {
    const r = await composeStackApi.parseAddons(content)
    const cfg = (r.data as AddonsConfig) || {}
    addonsConfig.traefik = cfg.traefik || {}
    addonsConfig.resources = cfg.resources || {}
  } catch {
    // Parsing failure (e.g. invalid YAML mid-edit) leaves the current
    // tab state intact — operators keep typing and the server parses
    // at the next save.
  }
}

// traefikCfgModel / resourcesCfgModel are the two-way bridges between the
// addon tabs and addonsConfig. Keeping them as computed getter/setter pairs
// avoids writing directly to nested reactive state from the template.
const traefikCfgModel = computed<Record<string, TraefikServiceCfg>>({
  get: () => addonsConfig.traefik || {},
  set: (v) => { addonsConfig.traefik = v },
})
const resourcesCfgModel = computed<Record<string, ResourcesServiceCfg>>({
  get: () => addonsConfig.resources || {},
  set: (v) => { addonsConfig.resources = v },
})

// historyRef is used after a restore to refresh the list so the new snapshot
// (reason=restore:revN) shows up immediately without a full page reload.
const historyRef = ref<InstanceType<typeof StackVersionHistory> | null>(null)

// reloadStackFromServer pulls the current content+envFile back from the API
// after a Restore operation. The restore already persisted server-side; this
// just syncs the editor state so the YAML editor shows the restored bytes.
async function reloadStackFromServer() {
  if (!model.id) return
  try {
    const s = await composeStackApi.find(model.id)
    if (s.data) {
      model.content = s.data.content || ''
      model.envFile = s.data.envFile || ''
    }
    // Rebuild addon tab state from the restored content.
    await reverseParseAddons(model.content)
  } catch {
    // Best-effort: the restore itself succeeded (server returned 2xx) so
    // we don't surface a second error if the follow-up fetch hiccups.
  }
}

async function validate(): Promise<boolean> {
  try {
    await (form.value as any).validate()
    return true
  } catch {
    return false
  }
}

// addonsPayload filters empty maps so the backend receives `undefined` when
// the operator didn't touch any wizard tab — a minor payload hygiene tweak.
function addonsPayload(): AddonsConfig | undefined {
  const traefik = addonsConfig.traefik && Object.keys(addonsConfig.traefik).length
    ? addonsConfig.traefik : undefined
  const resources = addonsConfig.resources && Object.keys(addonsConfig.resources).length
    ? addonsConfig.resources : undefined
  if (!traefik && !resources) return undefined
  return { traefik, resources }
}

async function saveStack() {
  if (!await validate()) return
  submitting.value = true
  try {
    const r = await composeStackApi.save(model, addonsPayload())
    message.success(t('buttons.save'))
    router.replace({ name: 'std_stack_edit', params: { id: r.data?.id || model.id } })
  } catch (e: any) {
    // Prefer the backend `info` field (coded errors from biz/compose_stack.go
    // — ErrStackNotFound / ErrHostUnreachable / ErrStackOperationFailed, etc.)
    // so operators see "stack foo: deploy failed on host local (unix:///…):
    // no such image" instead of axios' generic "Request failed 500".
    const info = e?.response?.data?.info || e?.message || String(e)
    message.error(info)
  } finally {
    submitting.value = false
  }
}

async function deployStack() {
  if (!await validate()) return
  submitting.value = true
  try {
    await composeStackApi.deploy(model, pullImages.value, addonsPayload())
    message.success(t('buttons.deploy'))
    router.push({ name: 'std_stack_list' })
  } catch (e: any) {
    const info = e?.response?.data?.info || e?.message || String(e)
    message.error(info)
  } finally {
    submitting.value = false
  }
}

// ---- Self-deploy Auto-Deploy branch --------------------------------------
//
// When the stack being edited is the one flagged as the self-deploy source
// stack in Settings, the Deploy button flips to "Auto-Deploy" and routes
// through /api/self-deploy/deploy instead of the usual stack deploy. The
// sidekick orchestrates the restart and the live progress iframe modal
// (shared composable) opens immediately.

const sdConfig = ref<SelfDeployConfig | null>(null)
// sdConfigLoaded gates the Deploy button so clicking it before the
// /api/self-deploy/load-config roundtrip resolves cannot accidentally
// fall through to the normal composeStack.deploy path — that path
// refuses to deploy a stack that contains the running Swirl instance
// and would leave a spurious "cannot deploy a stack that includes this
// Swirl instance" error on the record. See biz/compose_stack.go:~390.
const sdConfigLoaded = ref(false)
const autoDeployConfirm = ref(false)
// autoDeployError persists the last Auto-Deploy failure message so the
// operator sees the actual backend explanation (e.g. "source stack
// YAML is invalid: ...") instead of a brief toast that disappears.
const autoDeployError = ref('')

const isSelfDeployStack = computed(() => {
  return !!(sdConfig.value?.enabled && sdConfig.value?.sourceStackId === model.id)
})

const {
  progressOpen,
  progressStatus,
  progressDescription,
  progressElapsed,
  progressTimedOut,
  currentJobId,
  openProgressFromDeployResult,
} = useAutoDeployProgress()

async function loadSelfDeployConfig() {
  try {
    const r = await selfDeployApi.loadConfig()
    sdConfig.value = r?.data || null
  } catch {
    sdConfig.value = null
  } finally {
    // Flip the gate regardless of success/failure — a 403 still unlocks
    // the Deploy button in normal (non-auto) mode.
    sdConfigLoaded.value = true
  }
}

function autoDeployStack() {
  autoDeployConfirm.value = true
}

async function confirmAutoDeploy() {
  submitting.value = true
  autoDeployError.value = ''
  try {
    // Persist the editor state first: /api/self-deploy/deploy reads the
    // source stack's YAML from the DB, not from the browser. If the
    // operator edited (e.g. bumped the image tag) without clicking
    // Save, the sidekick would deploy the stale DB copy and the user
    // would see the unchanged tag redeploy. Saving before triggering
    // avoids that footgun entirely.
    if (!await validate()) {
      submitting.value = false
      return
    }
    await composeStackApi.save(model)
    const r = await selfDeployApi.deploy()
    autoDeployConfirm.value = false
    openProgressFromDeployResult(r?.data)
  } catch (e: any) {
    // Prefer the backend's `data.info` (coded errors include a clear
    // reason) over the axios generic `error.message` ("Request failed
    // with status code 500"). Persist the message so the operator can
    // read it after the toast fades.
    const info = e?.response?.data?.info
    const msg = (typeof info === 'string' && info.length > 0)
      ? info
      : (e?.message || String(e))
    autoDeployError.value = msg
    autoDeployConfirm.value = false
  } finally {
    submitting.value = false
  }
}


onMounted(async () => {
  const r = await hostApi.search('', '', 1, 1000)
  const data = r.data as any
  hosts.value = (data?.items || []).map((h: any) => ({ label: h.name, value: h.id, type: h.type }))

  if (isEdit.value) {
    const s = await composeStackApi.find(route.params.id as string)
    if (s.data) {
      model.id = s.data.id || ''
      model.hostId = s.data.hostId
      model.name = s.data.name
      model.content = s.data.content || ''
      model.envFile = s.data.envFile || ''
      model.errorMessage = s.data.errorMessage || ''
    }
    // Reverse-parse the persisted content so the addon tabs start in
    // sync with the YAML (marker-tagged labels/fields → tab state).
    await reverseParseAddons(model.content)
    // Fire in parallel: stack-level artifacts + self-deploy flag check.
    // loadSelfDeployConfig is tolerant to 403/404 (returns null) so
    // users without self_deploy.view still get a clean Deploy button.
    // Vault secrets + bindings are now loaded inside StackSecretsPanel
    // when the `secrets` tab is mounted (show-directive keeps it alive).
    await loadSelfDeployConfig()
  } else {
    model.content = '# Paste or author your docker-compose YAML here\n# example:\n# services:\n#   web:\n#     image: nginx:alpine\n#     ports:\n#       - "8080:80"\n'
    // A new stack cannot be the self-deploy source (no id yet) — flip
    // the gate so the Deploy button is clickable immediately.
    sdConfigLoaded.value = true
  }
})
</script>

<style scoped>
.muted {
  color: var(--n-text-color-3, #999);
}
.hash {
  font-size: 11px;
  opacity: 0.75;
}
.ads-iframe-fallback {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 24px;
  background: rgba(15, 19, 24, 0.92);
  color: #e4e8ee;
  text-align: center;
  font-size: 14px;
}
.ads-iframe-fallback a {
  color: #4b91ff;
  word-break: break-all;
}
</style>
