<template>
  <x-page-header>
    <template #action>
      <n-space :size="8">
        <n-button secondary size="small" @click="() => fetchData()">
          <template #icon>
            <n-icon><refresh-outline /></n-icon>
          </template>
          {{ t('buttons.refresh') || 'Refresh' }}
        </n-button>
        <!-- Bulk actions: visible when at least one container is selected -->
        <n-button
          v-if="checkedIds.length"
          secondary size="small" type="success"
          @click="bulkAction('start')"
        >{{ t('buttons.start') }} ({{ checkedIds.length }})</n-button>
        <n-button
          v-if="checkedIds.length"
          secondary size="small" type="warning"
          @click="bulkAction('stop')"
        >{{ t('buttons.stop') }} ({{ checkedIds.length }})</n-button>
        <n-button
          v-if="checkedIds.length"
          secondary size="small" type="info"
          @click="bulkAction('restart')"
        >{{ t('buttons.restart') }} ({{ checkedIds.length }})</n-button>
        <n-button
          v-if="checkedIds.length"
          secondary size="small" type="error"
          @click="bulkActionConfirm('delete')"
        >{{ t('buttons.delete') }} ({{ checkedIds.length }})</n-button>
        <n-button secondary size="small" type="warning" @click="prune" :disabled="!filter.node">
          <template #icon>
            <n-icon><close-icon /></n-icon>
          </template>
          {{ t('buttons.prune') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <x-empty-host-prompt v-if="showEmpty" :resource="t('objects.container', 2)" />
    <template v-else>
      <n-space :size="12">
        <n-select
          v-if="!isStandalone && nodes && nodes.length"
          filterable
          size="small"
          :consistent-menu-width="false"
          :placeholder="t('objects.node')"
          v-model:value="filter.node"
          :options="nodes"
          @update:value="() => fetchData()"
          style="width: 240px"
        />
        <n-select
          v-if="isStandalone && filter.node"
          size="small"
          :consistent-menu-width="false"
          :placeholder="t('objects.stack')"
          v-model:value="filter.project"
          :options="stackOptions"
          @update:value="() => fetchData()"
          style="width: 220px"
        />
        <n-select
          size="small"
          v-model:value="filter.status"
          :options="statusOptions"
          clearable
          :placeholder="t('container.all_states')"
          @update:value="() => fetchData()"
          style="width: 150px;"
        />
        <n-input size="small" v-model:value="filter.name" :placeholder="t('fields.name')" clearable />
        <n-button size="small" type="primary" @click="() => fetchData()">{{ t('buttons.search') }}</n-button>
      </n-space>
      <x-container-table
        :node="filter.node"
        :data="state.data as any"
        :loading="state.loading"
        :pagination="pagination"
        :show-stack-column="isStandalone"
        selectable
        :checked-keys="checkedIds"
        @update:checked-keys="(k: string[]) => checkedIds = k"
        @refresh="fetchData"
        @update-page-size="changePageSize"
      />
    </template>
  </n-space>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import {
  NSpace,
  NButton,
  NInput,
  NIcon,
  NSelect,
  useDialog,
  useMessage,
} from "naive-ui";
import {
  CloseOutline as CloseIcon,
  RefreshOutline,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import containerApi from "@/api/container";
import composeStackApi from "@/api/compose_stack";
import nodeApi from "@/api/node";
import XContainerTable from "@/components/ContainerTable.vue";
import XEmptyHostPrompt from "@/components/EmptyHostPrompt.vue";
import { useDataTable } from "@/utils/data-table";
import { formatSize } from "@/utils/render";
import { useStore } from "vuex";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const store = useStore()
const dialog = useDialog()
const message = useMessage()
const isStandalone = computed(() => store.state.mode === 'standalone')
const selectedHostId = computed(() => store.state.selectedHostId as string | null)
const filter = reactive({ node: '', name: '', project: '', status: '' })
const nodes: any = ref([])
const stackOptions: any = ref([])
const checkedIds = ref<string[]>([])
const showEmpty = computed(() => isStandalone.value && !selectedHostId.value)

const statusOptions = [
  { label: t('container.all_states'), value: '' },
  { label: 'Running', value: 'running' },
  { label: 'Exited', value: 'exited' },
  { label: 'Created', value: 'created' },
  { label: 'Paused', value: 'paused' },
]

const { state, pagination, fetchData: fetchDataRaw, changePageSize } = useDataTable(containerApi.search, filter, { remote: false, autoFetch: false })

function fetchData(page?: number) {
  checkedIds.value = []
  return fetchDataRaw(page)
}

async function loadStacks() {
  if (!isStandalone.value || !filter.node) {
    stackOptions.value = []
    return
  }
  try {
    const r = await composeStackApi.search({ hostId: filter.node, pageIndex: 1, pageSize: 1000 })
    const items = (r.data as any)?.items || []
    stackOptions.value = [
      { label: t('fields.all_stacks'), value: '' },
      ...items.map((s: any) => ({ label: s.name, value: s.name }))
    ]
  } catch {
    stackOptions.value = []
  }
}

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

async function bulkAction(action: 'start' | 'stop' | 'restart') {
  const ids = [...checkedIds.value]
  const errors: string[] = []
  for (const id of ids) {
    try {
      const name = (state.data as any[])?.find((c: any) => c.id === id)?.name || id
      switch (action) {
        case 'start': await containerApi.start(filter.node, id, name); break
        case 'stop': await containerApi.stop(filter.node, id, name); break
        case 'restart': await containerApi.restart(filter.node, id, name); break
      }
    } catch (e: any) {
      errors.push(`${id.substring(0, 12)}: ${e?.message || e}`)
    }
  }
  checkedIds.value = []
  if (errors.length) message.error(errors.join('\n'))
  else message.success(`${action}: ${ids.length} container(s)`)
  fetchData()
}

function bulkActionConfirm(action: 'delete' | 'kill') {
  dialog.warning({
    title: action === 'delete' ? t('buttons.delete') : t('buttons.kill'),
    content: t('prompts.delete'),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: async () => {
      const ids = [...checkedIds.value]
      const errors: string[] = []
      for (const id of ids) {
        try {
          const name = (state.data as any[])?.find((c: any) => c.id === id)?.name || id
          if (action === 'delete') await containerApi.delete(filter.node, id, name)
          else await containerApi.kill(filter.node, id, name)
        } catch (e: any) {
          errors.push(`${id.substring(0, 12)}: ${e?.message || e}`)
        }
      }
      checkedIds.value = []
      if (errors.length) message.error(errors.join('\n'))
      else message.success(`${action}: ${ids.length} container(s)`)
      fetchData()
    }
  })
}

watch(selectedHostId, async (v) => {
  if (v) {
    filter.node = v
    filter.project = ''
    await loadStacks()
    fetchData()
  }
})

onMounted(async () => {
  if (isStandalone.value) {
    if (selectedHostId.value) {
      filter.node = selectedHostId.value
      await loadStacks()
      fetchData()
    }
  } else {
    const r = await nodeApi.list(true)
    nodes.value = (r.data || []).map((n: any) => ({ label: n.name, value: n.id }))
    if (nodes.value.length) {
      filter.node = nodes.value[0].value
    }
    fetchData()
  }
})
</script>
