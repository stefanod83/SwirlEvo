<template>
  <x-page-header>
    <template #action>
      <n-space>
        <n-button secondary size="small" @click="$router.push({ name: 'std_stack_list' })">
          <template #icon>
            <n-icon><arrow-back-icon /></n-icon>
          </template>
          {{ t('buttons.return') }}
        </n-button>
        <n-button secondary size="small" type="primary" @click="$router.push({ name: 'std_stack_edit', params: { id: stack.id } })">
          <template #icon>
            <n-icon><create-outline /></n-icon>
          </template>
          {{ t('buttons.edit') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12" v-if="stack.id">
    <n-descriptions label-placement="left" bordered :column="2" size="small">
      <n-descriptions-item :label="t('fields.id')">{{ stack.id }}</n-descriptions-item>
      <n-descriptions-item :label="t('objects.host')">{{ stack.hostId }}</n-descriptions-item>
      <n-descriptions-item :label="t('fields.name')">{{ stack.name }}</n-descriptions-item>
      <n-descriptions-item :label="t('fields.status')">{{ stack.status }}</n-descriptions-item>
    </n-descriptions>
    <x-code :code="stack.content || ''" language="yaml" />
  </n-space>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  NSpace, NButton, NIcon, NDescriptions, NDescriptionsItem,
} from "naive-ui";
import {
  ArrowBackCircleOutline as ArrowBackIcon,
  CreateOutline,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XCode from "@/components/Code.vue";
import composeStackApi from "@/api/compose_stack";
import type { ComposeStack } from "@/api/compose_stack";
import { useRoute } from "vue-router";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute()
const stack = ref({} as ComposeStack)

onMounted(async () => {
  const r = await composeStackApi.find(route.params.id as string)
  stack.value = r.data || ({} as ComposeStack)
})
</script>
