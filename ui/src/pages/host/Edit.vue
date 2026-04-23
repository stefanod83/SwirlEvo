<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="onReturn">
        <template #icon>
          <n-icon><arrow-back-icon /></n-icon>
        </template>
        {{ t('buttons.return') }}
      </n-button>
    </template>
  </x-page-header>
  <div class="page-body">
    <n-space vertical :size="12">
      <!-- Connection panel — the core host identity + auth fields.
           Expanded by default so operators see (and fill) them on
           create and edit. -->
      <x-panel
        :title="t('host.panel_connection')"
        :subtitle="t('host.panel_connection_subtitle')"
        divider="bottom"
        :collapsed="panel !== 'connection'"
      >
        <template #action>
          <n-button
            secondary
            strong
            size="small"
            style="min-width: 75px"
            @click="togglePanel('connection')"
          >{{ panel === 'connection' ? t('buttons.collapse') : t('buttons.expand') }}</n-button>
        </template>
        <n-form ref="formRef" :model="model" :rules="rules" label-placement="left" label-width="120">
          <n-form-item :label="t('fields.name')" path="name">
            <n-input v-model:value="model.name" placeholder="My Docker Host" />
          </n-form-item>
          <n-form-item label="Endpoint" path="endpoint">
            <div style="width: 100%">
              <n-input v-model:value="model.endpoint"
                placeholder="tcp://host:2375 · unix:///var/run/docker.sock · ssh://user@host · https://swirl-swarm.example.com"
              />
              <div class="host-endpoint-hint">
                {{ t('tips.host_endpoint_types') }}
              </div>
            </div>
          </n-form-item>
          <n-alert v-if="isFederation" type="info" :show-icon="true" style="margin-bottom: 12px">
            {{ t('tips.host_federation_detected') }}
          </n-alert>
          <n-form-item v-if="!isFederation" label="Auth Method">
            <n-select v-model:value="model.authMethod" :options="authOptions" />
          </n-form-item>
          <!-- Federation-specific fields -->
          <n-form-item v-if="isFederation" :label="t('fields.swirl_token')" path="swirlToken">
            <n-input
              v-model:value="model.swirlToken"
              type="password"
              show-password-on="click"
              placeholder="Paste the federation peer token generated on the target Swirl"
            />
          </n-form-item>
          <n-form-item v-if="isFederation" :label="t('fields.token_auto_refresh')">
            <n-switch v-model:value="model.tokenAutoRefresh" />
          </n-form-item>
          <n-form-item v-if="isFederation && isEdit" :label="t('fields.token_status')">
            <n-space :size="8" align="center">
              <n-tag
                :type="tokenExpired ? 'error' : (tokenExpiringSoon ? 'warning' : 'success')"
                size="small"
              >
                {{ tokenExpired ? t('fields.token_expired') : (tokenExpiringSoon ? t('fields.token_expiring') : t('fields.token_valid')) }}
              </n-tag>
              <span v-if="model.tokenExpiresAt" class="muted">
                {{ t('fields.token_expires_at') }}: {{ formatDate(model.tokenExpiresAt) }}
              </span>
            </n-space>
          </n-form-item>
          <n-form-item v-if="model.authMethod === 'tcp+tls'" label="TLS CA Cert" path="tlsCaCert">
            <n-input v-model:value="model.tlsCaCert" type="textarea" :rows="3" placeholder="CA certificate (PEM)" />
          </n-form-item>
          <n-form-item v-if="model.authMethod === 'tcp+tls'" label="TLS Cert">
            <n-input v-model:value="model.tlsCert" type="textarea" :rows="3" placeholder="Client certificate (PEM)" />
          </n-form-item>
          <n-form-item v-if="model.authMethod === 'tcp+tls'" label="TLS Key">
            <n-input v-model:value="model.tlsKey" type="textarea" :rows="3" placeholder="Client key (PEM)" />
          </n-form-item>
          <n-form-item v-if="model.authMethod === 'ssh'" label="SSH User" path="sshUser">
            <n-input v-model:value="model.sshUser" placeholder="root" />
          </n-form-item>
          <n-form-item v-if="model.authMethod === 'ssh'" label="SSH Key">
            <n-input v-model:value="model.sshKey" type="textarea" :rows="3" placeholder="SSH private key (PEM)" />
          </n-form-item>
          <n-form-item :label="t('fields.color')">
            <div class="host-color-field">
              <div class="host-color-row">
                <n-color-picker
                  v-model:value="model.color"
                  :swatches="colorSwatches"
                  :show-alpha="false"
                  :modes="['hex']"
                  size="small"
                  class="host-color-picker"
                />
                <n-button
                  v-if="model.color"
                  size="small"
                  quaternary
                  @click="model.color = ''"
                  style="margin-left: 8px"
                >{{ t('buttons.clear') }}</n-button>
              </div>
              <div class="host-color-hint">
                {{ t('tips.host_color') }}
              </div>
            </div>
          </n-form-item>
          <n-form-item>
            <n-space>
              <n-button type="primary" @click="save">
                {{ t('buttons.save') }}
              </n-button>
              <n-button @click="testConnection" :loading="testing">
                Test Connection
              </n-button>
            </n-space>
          </n-form-item>
        </n-form>
        <n-alert v-if="testResult" :type="testResult.success ? 'success' : 'error'" :title="testResult.success ? 'Connection OK' : 'Connection Failed'" style="margin-top: 16px">
          <template v-if="testResult.success">
            {{ testResult.info?.hostname }} - Engine {{ testResult.info?.engineVersion }} ({{ testResult.info?.os }}/{{ testResult.info?.arch }})
          </template>
          <template v-else>{{ testResult.error }}</template>
        </n-alert>
      </x-panel>

      <!--
        Addon integrations — one panel per supported addon, parent-
        controlled collapse like the Settings page so the Host edit
        stays navigable as more addons land. Only surface in edit
        mode: at create time the host record doesn't exist yet, so
        AddonConfigExtract has nothing to attach to.
      -->
      <HostAddonTraefik
        v-if="isEdit && !isFederation && model.id"
        :host-id="model.id"
        :collapsed="panel !== 'traefik'"
        @toggle="togglePanel('traefik')"
      />
      <HostAddonGeneric
        v-if="isEdit && !isFederation && model.id"
        :host-id="model.id"
        addon-key="sablier"
        title="Sablier"
        :subtitle="t('host_addon_generic.sablier_subtitle')"
        :collapsed="panel !== 'sablier'"
        @toggle="togglePanel('sablier')"
      />
      <HostAddonGeneric
        v-if="isEdit && !isFederation && model.id"
        :host-id="model.id"
        addon-key="watchtower"
        title="Watchtower"
        :subtitle="t('host_addon_generic.watchtower_subtitle')"
        :collapsed="panel !== 'watchtower'"
        @toggle="togglePanel('watchtower')"
      />
      <HostAddonGeneric
        v-if="isEdit && !isFederation && model.id"
        :host-id="model.id"
        addon-key="backup"
        title="Backup"
        :subtitle="t('host_addon_generic.backup_subtitle')"
        :collapsed="panel !== 'backup'"
        @toggle="togglePanel('backup')"
      />
      <!-- Registry Cache is available for BOTH standalone hosts
           (daemon.json bootstrap path) AND swarm_via_swirl federated
           peers (Setting mirror-to-peer delegation). The component
           switches mode based on hostType. Excluded only at create
           time, when model.id is still empty. -->
      <HostAddonRegistryCache
        v-if="isEdit && model.id"
        :host-id="model.id"
        :host-type="model.type"
        :collapsed="panel !== 'registry_cache'"
        @toggle="togglePanel('registry_cache')"
      />
    </n-space>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, reactive } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NButton,
  NSpace,
  NAlert,
  NColorPicker,
  NIcon,
  NSwitch,
  NTag,
  useDialog,
  useMessage,
  type FormInst,
  type FormRules,
} from "naive-ui";
import { ArrowBackCircleOutline as ArrowBackIcon } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XPanel from "@/components/Panel.vue";
import HostAddonTraefik from "@/components/host-addons/HostAddonTraefik.vue";
import HostAddonGeneric from "@/components/host-addons/HostAddonGeneric.vue";
import HostAddonRegistryCache from "@/components/host-addons/HostAddonRegistryCache.vue";
import * as hostApi from "@/api/host";
import type { HostInfo } from "@/api/host";
import { useStore } from "vuex";
import { useI18n } from 'vue-i18n'
import { returnTo } from "@/utils/nav";
import { requiredRule, customRule, handleSaveError } from "@/utils/form";

const { t } = useI18n()
const route = useRoute();
const router = useRouter();
const store = useStore();
const dialog = useDialog();
const message = useMessage();
const formRef = ref<FormInst | null>(null);
const testing = ref(false);
const testResult = ref(null as null | { success: boolean; info?: HostInfo; error?: string });

// Active panel name — Settings-style single-expanded accordion so the
// Host edit stays navigable as more addons land. Defaults to
// 'connection' so the main form is visible on mount.
const panel = ref('connection')
function togglePanel(name: string) {
  panel.value = panel.value === name ? '' : name
}

const model = reactive({
  id: '',
  name: '',
  endpoint: '',
  authMethod: 'socket',
  tlsCaCert: '',
  tlsCert: '',
  tlsKey: '',
  sshUser: '',
  sshKey: '',
  color: '',
  // Federation (swarm_via_swirl) fields.
  swirlToken: '',
  tokenAutoRefresh: false,
  tokenExpiresAt: 0,
  type: '',
})

const isEdit = computed(() => !!route.params.id)

// Classify the endpoint to decide which form fields to render:
// https:// URLs are federation targets; the rest go through the
// standard direct-socket auth flow.
const isFederation = computed(() => {
  const ep = (model.endpoint || '').toLowerCase()
  return ep.startsWith('http://') || ep.startsWith('https://') || model.type === 'swarm_via_swirl'
})

const tokenExpired = computed(() => {
  if (!model.tokenExpiresAt) return false
  return model.tokenExpiresAt * 1000 < Date.now()
})
const tokenExpiringSoon = computed(() => {
  if (!model.tokenExpiresAt || tokenExpired.value) return false
  const days = (model.tokenExpiresAt * 1000 - Date.now()) / 86400_000
  return days < 7
})
function formatDate(ts: number): string {
  if (!ts) return ''
  return new Date(ts * 1000).toLocaleString()
}

// Curated palette — Naive UI's default colour picker exposes a
// rainbow that is too broad for a "pick a tag colour" UX. These 8
// swatches cover the semantic spectrum (safe/dev/staging/prod) while
// staying legible against both the light and dark layout themes.
const colorSwatches = [
  '#4b91ff', // blue — default development
  '#2ecc71', // green — staging ok
  '#f39c12', // orange — warning
  '#e74c3c', // red — production, be careful
  '#9b59b6', // purple — special-purpose hosts
  '#1abc9c', // teal
  '#34495e', // slate — infrastructure
  '#e67e22', // bronze
]

const authOptions = [
  { label: 'Docker Socket', value: 'socket' },
  { label: 'TCP (plain)', value: 'tcp' },
  { label: 'TCP + TLS', value: 'tcp+tls' },
  { label: 'SSH', value: 'ssh' },
]

// Form rules. Conditional rules (SSH User, Swirl Token, TLS CA)
// use customRule so they only fire when the relevant AuthMethod /
// federation state is active — otherwise NForm would reject valid
// submissions where those fields legitimately aren't rendered.
const rules = computed<FormRules>(() => ({
  name: requiredRule(),
  endpoint: requiredRule(),
  swirlToken: customRule(
    (_: any, v: string) => !isFederation.value || !!v,
    t('tips.required_rule'),
    undefined,
    isFederation.value,
  ),
  sshUser: customRule(
    (_: any, v: string) => model.authMethod !== 'ssh' || !!v,
    t('tips.required_rule'),
    undefined,
    model.authMethod === 'ssh',
  ),
  tlsCaCert: customRule(
    (_: any, v: string) => model.authMethod !== 'tcp+tls' || !!v,
    t('tips.required_rule'),
    undefined,
    model.authMethod === 'tcp+tls',
  ),
}))

function onReturn() {
  // On edit, returnTo prefers history.back if available so the user
  // lands on the exact list scroll position they came from. On new
  // (no id), fall back to the list root. Matches the pattern used by
  // compose_stack/Edit.vue::onReturn.
  if (route.params.id) {
    returnTo({ name: 'host_detail', params: { id: route.params.id as string } })
  } else {
    returnTo({ name: 'host_list' })
  }
}

async function save() {
  try {
    await formRef.value?.validate()
  } catch {
    // NForm rejects with the validation error list; the red field
    // feedback is already rendered — abort the submit silently.
    return
  }
  try {
    await hostApi.save(model)
    await store.dispatch('reloadHosts')
    router.push({ name: 'host_list' })
  } catch (e: any) {
    // Scheme-missing suggestion → dialog offers "apply and retry".
    if (handleSaveError(e, save, dialog, model)) return
    // Worker-rejected → the existing manager suggestions are surfaced
    // by the old path; here we just show the info field.
    const info = e?.response?.data?.info || e?.message || String(e)
    message.error(info)
  }
}

async function testConnection() {
  testing.value = true
  testResult.value = null
  try {
    const r = await hostApi.test(model.endpoint, model.authMethod)
    testResult.value = { success: true, info: r.data as HostInfo }
  } catch (e: any) {
    // Scheme-missing suggestion for the Test button too — apply
    // and re-test so operators confirm connectivity before saving.
    if (handleSaveError(e, testConnection, dialog, model)) {
      testing.value = false
      return
    }
    const info = e?.response?.data?.info || e?.message || String(e)
    testResult.value = { success: false, error: info }
  } finally {
    testing.value = false
  }
}

async function fetchData() {
  const id = route.params.id as string;
  if (id) {
    const r = await hostApi.find(id);
    if (r.data) {
      Object.assign(model, r.data)
    }
  }
}

onMounted(fetchData);
</script>

<style scoped>
/* Dedicated layout for the Color form row — previously the Clear
   button + hint were jammed on the same line as the colour picker
   input and overlapped at narrow widths. Vertical stack: row with
   picker + Clear, then the hint underneath on its own line. */
.host-color-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
}
.host-color-row {
  display: flex;
  align-items: center;
}
.host-color-picker {
  width: 180px;
  min-width: 160px;
}
.host-color-hint {
  font-size: 12px;
  color: var(--n-text-color-3, #888);
  line-height: 1.4;
}
.host-endpoint-hint {
  font-size: 12px;
  color: var(--n-text-color-3, #888);
  line-height: 1.4;
  margin-top: 6px;
}
</style>
