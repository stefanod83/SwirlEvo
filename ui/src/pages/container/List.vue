<template>
  <x-page-header>
    <template #action>
      <n-button secondary size="small" type="warning" @click="prune">
        <template #icon>
          <n-icon>
            <close-icon />
          </n-icon>
        </template>
        {{ t('buttons.prune') }}
      </n-button>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <n-space :size="12">
      <n-select
        filterable
        size="small"
        :consistent-menu-width="false"
        :placeholder="isStandalone ? t('objects.host') : t('objects.node')"
        v-model:value="filter.node"
        :options="nodes"
        @update:value="fetchData"
        style="width: 240px"
        v-if="nodes && nodes.length"
      />
      <n-input size="small" v-model:value="filter.name" :placeholder="t('fields.name')" clearable />
      <n-button size="small" type="primary" @click="() => fetchData()">{{ t('buttons.search') }}</n-button>
    </n-space>
    <n-data-table
      remote
      :row-key="(row: any) => row.id"
      size="small"
      :columns="columns"
      :data="state.data"
      :pagination="pagination"
      :loading="state.loading"
      @update:page="fetchData"
      @update-page-size="changePageSize"
      scroll-x="max-content"
    />
  </n-space>
</template>

<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from "vue";
import {
  NSpace,
  NButton,
  NButtonGroup,
  NDataTable,
  NInput,
  NSelect,
  NIcon,
  NTooltip,
  useDialog,
  useMessage,
} from "naive-ui";
import {
  CloseOutline as CloseIcon,
  PlayOutline,
  StopOutline,
  RefreshOutline,
  PauseOutline,
  FlashOffOutline,
  TrashOutline,
  EyeOutline,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import containerApi from "@/api/container";
import type { Container } from "@/api/container";
import { listHostsOrNodes } from "@/utils/host-node";
import { useDataTable } from "@/utils/data-table";
import { formatSize, renderLink, renderTag } from "@/utils/render";
import { useRouter } from "vue-router";
import { useStore } from "vuex";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const store = useStore()
const dialog = useDialog()
const message = useMessage()
const isStandalone = computed(() => store.state.mode === 'standalone')
const filter = reactive({ node: '', name: '' });
const nodes: any = ref([])

function actionButton(type: 'default' | 'error' | 'warning' | 'success' | 'info', iconCmp: any, tooltip: string, disabled: boolean, onClick: () => void) {
  return h(NTooltip, { trigger: 'hover' }, {
    trigger: () => h(NButton, {
      size: 'tiny', quaternary: true, type, disabled, onClick,
    }, { icon: () => h(NIcon, null, { default: () => h(iconCmp) }) }),
    default: () => tooltip,
  })
}

async function runAction(fn: () => Promise<any>, successMsg: string) {
  try {
    await fn();
    message.success(successMsg);
    await fetchData();
  } catch (e: any) {
    message.error(e?.message || String(e))
  }
}

function confirmDelete(c: Container) {
  dialog.warning({
    title: t('buttons.delete'),
    content: t('prompts.delete'),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: () => runAction(() => containerApi.delete(filter.node, c.id, c.name), t('buttons.delete')),
  })
}

const columns = [
  {
    title: t('fields.name'),
    key: "name",
    fixed: "left" as const,
    render: (c: Container) => {
      const node = c.labels?.find(l => l.name === 'com.docker.swarm.node.id')
      const name = c.name.length > 32 ? c.name.substring(0, 32) + '...' : c.name
      return renderLink({ name: 'container_detail', params: { id: c.id, node: node?.value || filter.node || '-' } }, name)
    },
  },
  {
    title: t('objects.image'),
    key: "image",
  },
  {
    title: t('fields.state'),
    key: "state",
    render(c: Container) {
      const type = c.state === 'running' ? 'success' : (c.state === 'paused' ? 'warning' : 'error')
      return renderTag(c.state, type as any)
    }
  },
  {
    title: t('fields.status'),
    key: "status",
  },
  {
    title: t('fields.created_at'),
    key: "createdAt"
  },
  {
    title: t('fields.actions'),
    key: "actions",
    width: 320,
    render(c: Container) {
      const running = c.state === 'running'
      const paused = c.state === 'paused'
      const buttons = [
        actionButton('success', PlayOutline, t('buttons.start'), running || paused,
          () => runAction(() => containerApi.start(filter.node, c.id, c.name), t('buttons.start'))),
        actionButton('warning', StopOutline, t('buttons.stop'), !running,
          () => runAction(() => containerApi.stop(filter.node, c.id, c.name), t('buttons.stop'))),
        actionButton('info', RefreshOutline, t('buttons.restart'), !running,
          () => runAction(() => containerApi.restart(filter.node, c.id, c.name), t('buttons.restart'))),
        actionButton('warning', PauseOutline, paused ? t('buttons.unpause') : t('buttons.pause'), !running && !paused,
          () => runAction(() => paused ? containerApi.unpause(filter.node, c.id, c.name) : containerApi.pause(filter.node, c.id, c.name), paused ? t('buttons.unpause') : t('buttons.pause'))),
        actionButton('error', FlashOffOutline, t('buttons.kill'), !running,
          () => runAction(() => containerApi.kill(filter.node, c.id, c.name), t('buttons.kill'))),
        actionButton('default', EyeOutline, t('buttons.view') || 'Details', false,
          () => router.push({ name: 'container_detail', params: { id: c.id, node: filter.node || '-' } })),
        actionButton('error', TrashOutline, t('buttons.delete'), running || paused, () => confirmDelete(c)),
      ]
      return h(NButtonGroup, null, { default: () => buttons })
    },
  },
];
const { state, pagination, fetchData, changePageSize } = useDataTable(containerApi.search, filter, false)

async function prune() {
  dialog.warning({
    title: t('dialogs.prune_container.title'),
    content: t('dialogs.prune_container.body'),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: async () => {
      const r = await containerApi.prune(filter.node);
      message.info(t('texts.prune_container_success', {
        count: r.data?.count,
        size: formatSize(r.data?.size as number),
      }));
      fetchData();
    }
  })
}

onMounted(async () => {
  nodes.value = await listHostsOrNodes()
  if (nodes.value.length) {
    filter.node = nodes.value[0].value
  }
  fetchData()
})
</script>
