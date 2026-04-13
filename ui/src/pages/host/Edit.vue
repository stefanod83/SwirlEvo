<template>
  <x-page-header />
  <div class="page-body">
    <n-form :model="model" label-placement="left" label-width="120">
      <n-form-item :label="t('fields.name')" required>
        <n-input v-model:value="model.name" placeholder="My Docker Host" />
      </n-form-item>
      <n-form-item label="Endpoint" required>
        <n-input v-model:value="model.endpoint" placeholder="tcp://192.168.1.100:2375 or unix:///var/run/docker.sock" />
      </n-form-item>
      <n-form-item label="Auth Method">
        <n-select v-model:value="model.authMethod" :options="authOptions" />
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
import { onMounted, ref, reactive } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NButton,
  NSpace,
  NAlert,
} from "naive-ui";
import XPageHeader from "@/components/PageHeader.vue";
import * as hostApi from "@/api/host";
import type { HostInfo } from "@/api/host";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute();
const router = useRouter();
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
})

const authOptions = [
  { label: 'Docker Socket', value: 'socket' },
  { label: 'TCP (plain)', value: 'tcp' },
  { label: 'TCP + TLS', value: 'tcp+tls' },
  { label: 'SSH', value: 'ssh' },
]

async function save() {
  await hostApi.save(model);
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
