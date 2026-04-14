<template>
  <n-data-table
    :row-key="(row: any) => row.id"
    size="small"
    :columns="columns"
    :data="data"
    :pagination="pagination"
    :loading="loading"
    remote
    @update:page="(p: number) => $emit('update:page', p)"
    @update-page-size="(s: number) => $emit('update-page-size', s)"
    scroll-x="max-content"
  />
</template>

<script setup lang="ts">
import { h, computed } from "vue";
import {
  NDataTable, NButton, NButtonGroup, NIcon, NTooltip,
  useDialog, useMessage,
} from "naive-ui";
import {
  PlayOutline, StopOutline, RefreshOutline, PauseOutline,
  FlashOffOutline, TrashOutline, EyeOutline,
} from "@vicons/ionicons5";
import containerApi from "@/api/container";
import type { Container } from "@/api/container";
import { renderLink, renderTag } from "@/utils/render";
import { useRouter } from "vue-router";
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  node: string;
  data: Container[];
  loading?: boolean;
  pagination?: any;
  showStackColumn?: boolean;
}>()

const emit = defineEmits<{
  (e: 'refresh'): void;
  (e: 'update:page', p: number): void;
  (e: 'update-page-size', s: number): void;
}>()

const { t } = useI18n()
const router = useRouter()
const dialog = useDialog()
const message = useMessage()

function actionButton(type: 'default' | 'error' | 'warning' | 'success' | 'info', iconCmp: any, tooltip: string, disabled: boolean, onClick: () => void) {
  return h(NTooltip, { trigger: 'hover' }, {
    trigger: () => h(NButton, { size: 'tiny', quaternary: true, type, disabled, onClick }, { icon: () => h(NIcon, null, { default: () => h(iconCmp) }) }),
    default: () => tooltip,
  })
}

async function runAction(fn: () => Promise<any>, msg: string) {
  try { await fn(); message.success(msg); emit('refresh') }
  catch (e: any) { message.error(e?.message || String(e)) }
}

function confirmDelete(c: Container) {
  dialog.warning({
    title: t('buttons.delete'),
    content: t('prompts.delete'),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: () => runAction(() => containerApi.delete(props.node, c.id, c.name), t('buttons.delete')),
  })
}

function projectOf(c: Container): string {
  return c.labels?.find(l => l.name === 'com.docker.compose.project')?.value || ''
}

const columns = computed(() => {
  const base: any[] = [
    {
      title: t('fields.state'),
      key: "state",
      fixed: "left" as const,
      width: 90,
      render(c: Container) {
        const type = c.state === 'running' ? 'success' : (c.state === 'paused' ? 'warning' : 'error')
        return renderTag(c.state, type as any)
      }
    },
    {
      title: t('fields.name'),
      key: "name",
      render: (c: Container) => {
        const node = c.labels?.find(l => l.name === 'com.docker.swarm.node.id')
        const name = c.name.length > 32 ? c.name.substring(0, 32) + '...' : c.name
        return renderLink({ name: 'container_detail', params: { id: c.id, node: node?.value || props.node || '-' } }, name)
      },
    },
    {
      title: t('objects.image'),
      key: "image",
    },
  ]
  if (props.showStackColumn) {
    base.push({
      title: t('objects.stack'),
      key: "stack",
      render: (c: Container) => {
        const project = projectOf(c)
        if (!project) return ''
        return renderLink({ name: 'std_stack_external_detail', params: { hostId: props.node, name: project } }, project)
      },
    })
  }
  base.push(
    {
      title: t('fields.status'),
      key: "status",
    },
    {
      title: t('fields.created_at'),
      key: "createdAt",
    },
    {
      title: t('fields.actions'),
      key: "actions",
      width: 260,
      render(c: Container) {
        const running = c.state === 'running'
        const paused = c.state === 'paused'
        const buttons = [
          actionButton('success', PlayOutline, t('buttons.start'), running || paused,
            () => runAction(() => containerApi.start(props.node, c.id, c.name), t('buttons.start'))),
          actionButton('warning', StopOutline, t('buttons.stop'), !running,
            () => runAction(() => containerApi.stop(props.node, c.id, c.name), t('buttons.stop'))),
          actionButton('info', RefreshOutline, t('buttons.restart'), !running,
            () => runAction(() => containerApi.restart(props.node, c.id, c.name), t('buttons.restart'))),
          actionButton('warning', PauseOutline, paused ? t('buttons.unpause') : t('buttons.pause'), !running && !paused,
            () => runAction(() => paused ? containerApi.unpause(props.node, c.id, c.name) : containerApi.pause(props.node, c.id, c.name), paused ? t('buttons.unpause') : t('buttons.pause'))),
          actionButton('error', FlashOffOutline, t('buttons.kill'), !running,
            () => runAction(() => containerApi.kill(props.node, c.id, c.name), t('buttons.kill'))),
          actionButton('default', EyeOutline, t('buttons.view'), false,
            () => router.push({ name: 'container_detail', params: { id: c.id, node: props.node || '-' } })),
          actionButton('error', TrashOutline, t('buttons.delete'), running || paused, () => confirmDelete(c)),
        ]
        return h(NButtonGroup, null, { default: () => buttons })
      },
    }
  )
  return base
})
</script>
