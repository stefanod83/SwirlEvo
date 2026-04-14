<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'std_stack_list' })">
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
      <n-form-item :label="t('fields.content')" path="content">
        <x-code-mirror
          v-model:value="model.content"
          mode="yaml"
          :style="{ width: '100%', height: '55vh', border: '1px solid #ddd' }"
        />
      </n-form-item>
      <n-space>
        <n-checkbox v-model:checked="pullImages">{{ t('fields.pull_images') || 'Pull images' }}</n-checkbox>
      </n-space>
    </n-form>

    <n-space>
      <n-button type="primary" :loading="submitting" @click="deployStack">
        <template #icon>
          <n-icon><rocket-outline /></n-icon>
        </template>
        {{ t('buttons.deploy') }}
      </n-button>
      <n-button secondary :loading="submitting" @click="saveStack">
        <template #icon>
          <n-icon><save-outline /></n-icon>
        </template>
        {{ t('buttons.save') }}
      </n-button>
    </n-space>
  </n-space>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import {
  NSpace, NButton, NForm, NFormItem, NFormItemGi, NGrid, NInput, NSelect, NCheckbox, NIcon,
  useMessage,
} from "naive-ui";
import {
  ArrowBackCircleOutline as ArrowBackIcon,
  RocketOutline, SaveOutline,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XCodeMirror from "@/components/CodeMirror.vue";
import composeStackApi from "@/api/compose_stack";
import type { ComposeStack } from "@/api/compose_stack";
import * as hostApi from "@/api/host";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'
import { requiredRule } from "@/utils/form";

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const message = useMessage()
const form = ref()
const submitting = ref(false)
const pullImages = ref(false)
const isEdit = computed(() => !!route.params.id)

const model = reactive({
  id: '',
  hostId: '',
  name: '',
  content: '',
} as ComposeStack)

const hosts: any = ref([])
const rules = {
  hostId: requiredRule(),
  name: requiredRule(),
  content: requiredRule(),
}

async function validate(): Promise<boolean> {
  try {
    await (form.value as any).validate()
    return true
  } catch {
    return false
  }
}

async function saveStack() {
  if (!await validate()) return
  submitting.value = true
  try {
    const r = await composeStackApi.save(model)
    message.success(t('buttons.save'))
    router.replace({ name: 'std_stack_edit', params: { id: r.data?.id || model.id } })
  } catch (e: any) {
    message.error(e?.message || String(e))
  } finally {
    submitting.value = false
  }
}

async function deployStack() {
  if (!await validate()) return
  submitting.value = true
  try {
    await composeStackApi.deploy(model, pullImages.value)
    message.success(t('buttons.deploy'))
    router.push({ name: 'std_stack_list' })
  } catch (e: any) {
    message.error(e?.message || String(e))
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  const r = await hostApi.search('', '', 1, 1000)
  const data = r.data as any
  hosts.value = (data?.items || []).map((h: any) => ({ label: h.name, value: h.id }))

  if (isEdit.value) {
    const s = await composeStackApi.find(route.params.id as string)
    if (s.data) {
      model.id = s.data.id || ''
      model.hostId = s.data.hostId
      model.name = s.data.name
      model.content = s.data.content || ''
    }
  } else {
    model.content = '# Paste or author your docker-compose YAML here\n# example:\n# services:\n#   web:\n#     image: nginx:alpine\n#     ports:\n#       - "8080:80"\n'
  }
})
</script>
