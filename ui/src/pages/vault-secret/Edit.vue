<template>
  <x-page-header :subtitle="model.name">
    <template #action>
      <n-space :size="8">
        <n-button secondary size="small" @click="$router.push({ name: 'vault_secret_list' })">
          <template #icon>
            <n-icon><back-icon /></n-icon>
          </template>
          {{ t('buttons.return') }}
        </n-button>
        <n-button
          v-if="model.id"
          secondary
          size="small"
          :loading="previewing"
          @click="doPreview"
        >
          <template #icon>
            <n-icon><eye-icon /></n-icon>
          </template>
          {{ t('buttons.preview') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <n-alert type="info" :show-icon="false">
      {{ t('tips.vault_secret') }}
    </n-alert>
    <n-alert type="warning" :show-icon="false">
      {{ t('tips.vault_secret_no_value') }}
    </n-alert>
    <n-form :model="model" :rules="rules" ref="form" label-placement="top">
      <n-grid cols="1 640:2" :x-gap="24">
        <n-form-item-gi :label="t('fields.name')" path="name">
          <n-input
            v-model:value="model.name"
            :placeholder="t('tips.vault_secret_name')"
            :disabled="Boolean(model.id)"
          />
        </n-form-item-gi>
        <n-form-item-gi :label="t('fields.path')" path="path">
          <n-input v-model:value="model.path" :placeholder="t('tips.vault_secret_path')" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('fields.field')" path="field">
          <n-input v-model:value="model.field" :placeholder="t('tips.vault_secret_field')" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('fields.desc')" path="desc">
          <n-input
            v-model:value="model.desc"
            type="textarea"
            :autosize="{ minRows: 1, maxRows: 4 }"
          />
        </n-form-item-gi>
        <n-form-item-gi :label="t('fields.labels')" path="labels" span="2">
          <n-dynamic-input
            v-model:value="labels"
            #="{ value }"
            :on-create="newPair"
          >
            <n-input :placeholder="t('fields.name')" v-model:value="value.name" />
            <div style="height: 34px; line-height: 34px; margin: 0 8px">=</div>
            <n-input :placeholder="t('fields.value')" v-model:value="value.value" />
          </n-dynamic-input>
        </n-form-item-gi>
        <n-gi :span="2">
          <n-button
            type="primary"
            :disabled="submiting"
            :loading="submiting"
            @click.prevent="submit"
          >
            <template #icon>
              <n-icon><save-icon /></n-icon>
            </template>
            {{ t('buttons.save') }}
          </n-button>
        </n-gi>
      </n-grid>
    </n-form>
    <n-alert v-if="previewResult && previewResult.exists" type="success">
      <div>{{ t('texts.vault_secret_available_fields') }}:</div>
      <n-space :size="4" style="margin-top: 8px;">
        <n-tag v-for="f of previewResult.fields" :key="f" size="small" type="info">{{ f }}</n-tag>
        <span v-if="!previewResult.fields.length" class="muted">
          {{ t('texts.vault_secret_fields_empty') }}
        </span>
      </n-space>
    </n-alert>
    <n-alert v-else-if="previewResult && !previewResult.exists" type="error">
      {{ t('texts.vault_secret_missing') }}
    </n-alert>
    <n-alert v-if="previewError" type="error">
      {{ previewError }}
    </n-alert>
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
  NGrid,
  NGi,
  NFormItemGi,
  NDynamicInput,
  NAlert,
  NTag,
} from "naive-ui";
import {
  ArrowBackCircleOutline as BackIcon,
  SaveOutline as SaveIcon,
  EyeOutline as EyeIcon,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import vaultSecretApi from "@/api/vault-secret";
import type { VaultSecret, VaultSecretPreview } from "@/api/vault-secret";
import { useRoute } from "vue-router";
import { router } from "@/router/router";
import { useForm, requiredRule, customRule } from "@/utils/form";
import { useI18n } from 'vue-i18n'

interface LabelPair {
  name: string;
  value: string;
}

const { t } = useI18n()
const route = useRoute()
const form = ref()
const model = ref({} as VaultSecret)
const labels = ref([] as LabelPair[])
const previewing = ref(false)
const previewResult = ref<VaultSecretPreview | null>(null)
const previewError = ref("")

// Name must be a single segment (no slashes), max 128 chars.
const nameSegmentRule = customRule(
  (_r, v) => !v || (/^[A-Za-z0-9._-]+$/.test(v) && v.length <= 128),
  t('tips.vault_secret_name_rule'),
)

const rules: any = {
  name: [requiredRule(), nameSegmentRule],
  path: requiredRule(),
}

function newPair(): LabelPair {
  return { name: "", value: "" }
}

function pairsToMap(pairs: LabelPair[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const p of pairs) {
    const k = (p.name || "").trim()
    if (k) out[k] = p.value ?? ""
  }
  return out
}

function mapToPairs(m?: Record<string, string>): LabelPair[] {
  if (!m) return []
  return Object.keys(m).map(k => ({ name: k, value: m[k] }))
}

async function saveAction() {
  const payload: Partial<VaultSecret> = {
    ...model.value,
    labels: pairsToMap(labels.value),
  }
  return vaultSecretApi.save(payload)
}

const { submit, submiting } = useForm(form, saveAction, () => {
  window.message.info(t('texts.action_success'))
  router.push({ name: 'vault_secret_list' })
})

async function doPreview() {
  previewing.value = true
  previewError.value = ""
  previewResult.value = null
  try {
    const r = await vaultSecretApi.preview(model.value.id)
    previewResult.value = r.data as VaultSecretPreview
  } catch (e: any) {
    previewError.value = e?.message || String(e)
  } finally {
    previewing.value = false
  }
}

async function fetchData() {
  const id = route.params.id as string
  if (id) {
    const r = await vaultSecretApi.find(id)
    if (r.data) {
      model.value = r.data as VaultSecret
      labels.value = mapToPairs(model.value.labels)
    }
  }
}

onMounted(fetchData)
</script>

<style scoped>
.muted {
  color: var(--n-text-color-3, #999);
}
</style>
