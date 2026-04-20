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
    <n-form :model="model" label-placement="left" label-width="120">
      <n-form-item :label="t('fields.name')" required>
        <n-input v-model:value="model.name" placeholder="My Docker Host" />
      </n-form-item>
      <n-form-item label="Endpoint" required>
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
      <n-form-item v-if="isFederation" :label="t('fields.swirl_token')" required>
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
      <n-form-item v-if="model.authMethod === 'tcp+tls'" label="TLS CA Cert">
        <n-input v-model:value="model.tlsCaCert" type="textarea" :rows="3" placeholder="CA certificate (PEM)" />
      </n-form-item>
      <n-form-item v-if="model.authMethod === 'tcp+tls'" label="TLS Cert">
        <n-input v-model:value="model.tlsCert" type="textarea" :rows="3" placeholder="Client certificate (PEM)" />
      </n-form-item>
      <n-form-item v-if="model.authMethod === 'tcp+tls'" label="TLS Key">
        <n-input v-model:value="model.tlsKey" type="textarea" :rows="3" placeholder="Client key (PEM)" />
      </n-form-item>
      <n-form-item v-if="model.authMethod === 'ssh'" label="SSH User">
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
} from "naive-ui";
import { ArrowBackCircleOutline as ArrowBackIcon } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import * as hostApi from "@/api/host";
import type { HostInfo } from "@/api/host";
import { useStore } from "vuex";
import { useI18n } from 'vue-i18n'
import { returnTo } from "@/utils/nav";

const { t } = useI18n()
const route = useRoute();
const router = useRouter();
const store = useStore();
const testing = ref(false);
const testResult = ref(null as null | { success: boolean; info?: HostInfo; error?: string });

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
  await hostApi.save(model);
  await store.dispatch('reloadHosts')
  router.push({ name: 'host_list' })
}

async function testConnection() {
  testing.value = true
  testResult.value = null
  try {
    const r = await hostApi.test(model.endpoint)
    testResult.value = { success: true, info: r.data as HostInfo }
  } catch (e: any) {
    testResult.value = { success: false, error: e.message || String(e) }
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
