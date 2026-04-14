<template>
  <n-result :status="status" :title="titleText" :description="descriptionText" style="padding:48px 16px">
    <template #icon>
      <n-icon :component="ServerOutline" size="48" />
    </template>
  </n-result>
</template>

<script setup lang="ts">
import { NResult, NIcon } from "naive-ui";
import { ServerOutline } from "@vicons/ionicons5";
import { useI18n } from 'vue-i18n'
import { computed } from "vue";

const { t } = useI18n()
const props = defineProps<{
  resource?: string
}>()

const status = 'info' as const
const titleText = computed(() => t('texts.select_host_title') || 'Select a host')
const descriptionText = computed(() => {
  const base = t('texts.select_host_body') || 'Choose a host from the top bar to see its resources.'
  return props.resource ? base.replace('its resources', props.resource) : base
})
</script>
