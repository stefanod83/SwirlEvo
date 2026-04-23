<template>
  <x-page-header :subtitle="model.name">
    <template #action>
      <n-button secondary size="small" @click="onReturn">
        <template #icon>
          <n-icon>
            <back-icon />
          </n-icon>
        </template>
        {{ t('buttons.return') }}
      </n-button>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <n-form :model="model" ref="form" :rules="rules">
      <n-grid cols="1 640:2" :x-gap="24">
        <n-form-item-gi :label="t('fields.name')" path="name">
          <n-input :placeholder="t('fields.name')" v-model:value="model.name" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('fields.url')" path="url">
          <n-input :placeholder="t('tips.registry_url')" v-model:value="model.url" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('fields.login_name')" path="username">
          <n-input :placeholder="t('fields.login_name')" v-model:value="model.username" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('fields.password')" path="password">
          <n-input type="password" :placeholder="t('fields.password')" v-model:value="model.password" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('registry.skip_tls_verify')" path="skipTlsVerify">
          <n-checkbox v-model:checked="model.skipTlsVerify">
            {{ t('registry.skip_tls_verify_hint') }}
          </n-checkbox>
        </n-form-item-gi>
        <!-- Optional CA PEM: distributed to managed hosts by the
             Registry Cache bootstrap when this Registry is linked as
             the mirror source. Plain text — public material. -->
        <n-gi :span="2">
          <n-form-item :label="t('registry.ca_cert_pem')" path="caCertPem">
            <n-input
              type="textarea"
              :autosize="{ minRows: 3, maxRows: 10 }"
              placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
              v-model:value="model.caCertPem"
            />
          </n-form-item>
          <div
            v-if="model.caFingerprint"
            style="margin: -6px 0 12px 0; padding: 6px 10px; font-size: 12px; color: var(--n-text-color-3, #666); background-color: rgba(128,128,128,0.06); border-left: 3px solid rgba(64,128,255,0.45); border-radius: 4px"
          >
            <strong>{{ t('registry.ca_fingerprint') }}:</strong>
            <code style="font-size: 11px; word-break: break-all">{{ model.caFingerprint }}</code>
          </div>
        </n-gi>
        <n-gi :span="2">
          <n-button
            @click.prevent="submit"
            type="primary"
            :disabled="submiting"
            :loading="submiting"
          >
            <template #icon>
              <n-icon>
                <save-icon />
              </n-icon>
            </template>
            {{ t('buttons.save') }}
          </n-button>
        </n-gi>
      </n-grid>
    </n-form>
  </n-space>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  NButton,
  NSpace,
  NInput,
  NIcon,
  NForm,
  NFormItem,
  NGrid,
  NGi,
  NFormItemGi,
  NCheckbox,
} from "naive-ui";
import {
  ArrowBackCircleOutline as BackIcon,
  SaveOutline as SaveIcon,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import registryApi from "@/api/registry";
import type { Registry } from "@/api/registry";
import { useRoute } from "vue-router";
import { router } from "@/router/router";
import { useForm, requiredRule, urlRule } from "@/utils/form";
import { returnTo } from "@/utils/nav";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute();
const model = ref({} as Registry);

function onReturn() {
  const id = route.params.id as string
  if (id) {
    returnTo({ name: 'registry_detail', params: { id } })
  } else {
    returnTo({ name: 'registry_list' })
  }
}
const form = ref();
const rules: any = {
  name: requiredRule(),
  url: [requiredRule(), urlRule()],
  username: requiredRule(),
};
const { submit, submiting } = useForm(form, () => registryApi.save(model.value), () => {
  window.message.info(t('texts.action_success'));
  router.push({ name: 'registry_list' })
})

async function fetchData() {
  const id = route.params.id as string
  if (id) {
    let r = await registryApi.find(id);
    model.value = r.data as Registry;
  }
}

onMounted(fetchData);
</script>
