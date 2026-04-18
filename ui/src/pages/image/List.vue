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
        <n-button secondary size="small" type="error" :disabled="!checkedIds.length" @click="bulkDelete(false)">
          <template #icon>
            <n-icon><trash-outline /></n-icon>
          </template>
          {{ t('buttons.delete') }} ({{ checkedIds.length }})
        </n-button>
        <n-button secondary size="small" type="error" :disabled="!checkedIds.length" @click="bulkDelete(true)">
          <template #icon>
            <n-icon><flash-off-outline /></n-icon>
          </template>
          {{ t('buttons.force_delete') || 'Force delete' }} ({{ checkedIds.length }})
        </n-button>
        <n-button secondary size="small" type="warning" @click="prune">
          <template #icon>
            <n-icon><close-icon /></n-icon>
          </template>
          {{ t('buttons.prune') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <x-empty-host-prompt v-if="showEmpty" :resource="t('objects.image', 2)" />
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
          style="width: 200px"
        />
        <n-input size="small" v-model:value="filter.name" :placeholder="t('fields.name')" clearable />
        <n-button size="small" type="primary" @click="() => fetchData()">{{ t('buttons.search') }}</n-button>
      </n-space>
      <n-data-table
        remote
        :row-key="(row: any) => row.id"
        size="small"
        :columns="columns"
        :data="paginatedData"
        :pagination="pagination"
        :loading="state.loading"
        :checked-row-keys="checkedIds"
        @update:checked-row-keys="(k: any) => checkedIds = k"
        @update:page="changePage"
        @update-page-size="changePageSize"
        @update:sorter="handleSorterChange"
        scroll-x="max-content"
      />
    </template>
  </n-space>

  <!-- Tag dialog -->
  <n-modal
    v-model:show="tagDialog.show"
    preset="dialog"
    :title="t('image.tag_title')"
    :positive-text="t('buttons.save')"
    :negative-text="t('buttons.cancel')"
    :loading="tagDialog.loading"
    @positive-click="doTag"
  >
    <n-space vertical :size="8">
      <div>{{ t('image.tag_source') }}: <code class="mono">{{ tagDialog.source }}</code></div>
      <n-input
        v-model:value="tagDialog.target"
        :placeholder="t('image.tag_target_placeholder')"
      />
    </n-space>
  </n-modal>

  <!-- Push dialog -->
  <n-modal
    v-model:show="pushDialog.show"
    preset="dialog"
    style="width: 520px"
    :title="t('image.push_title')"
    :positive-text="t('image.push_submit')"
    :negative-text="t('buttons.cancel')"
    :loading="pushDialog.loading"
    @positive-click="doPush"
  >
    <n-space vertical :size="8">
      <n-form-item :label="t('image.push_ref')" :show-feedback="false">
        <n-select
          v-model:value="pushDialog.ref"
          :options="pushDialog.refOptions"
          :placeholder="t('image.push_ref_placeholder')"
          filterable
          tag
        />
      </n-form-item>
      <n-form-item :label="t('image.select_registry')" :show-feedback="false">
        <n-select
          v-model:value="pushDialog.registryId"
          :options="registryOptions"
          :placeholder="t('image.select_registry_placeholder')"
          clearable
        />
      </n-form-item>
      <n-alert type="warning" :show-icon="false">
        {{ t('image.push_warning') }}
      </n-alert>
    </n-space>
  </n-modal>
</template>

<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NSpace,
  NButton,
  NButtonGroup,
  NDataTable,
  NInput,
  NSelect,
  NIcon,
  NTag,
  NTooltip,
  NModal,
  NFormItem,
  NAlert,
  useDialog,
  useMessage,
} from "naive-ui";
import {
  CloseOutline as CloseIcon,
  TrashOutline,
  FlashOffOutline,
  RefreshOutline,
  PricetagOutline,
  CloudUploadOutline,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import imageApi from "@/api/image";
import type { Image } from "@/api/image";
import nodeApi from "@/api/node";
import { useStore } from "vuex";
import XEmptyHostPrompt from "@/components/EmptyHostPrompt.vue";
import { computed, watch } from "vue";
import { useDataTable } from "@/utils/data-table";
import { formatSize, renderLink, renderTags } from "@/utils/render";
import { useI18n } from 'vue-i18n'
import { useImageActions } from "./useImageActions";

const { t } = useI18n()
const dialog = useDialog()
const message = useMessage()
const store = useStore()
const isStandalone = computed(() => store.state.mode === 'standalone')
const selectedHostId = computed(() => store.state.selectedHostId as string | null)
const filter = reactive({
  node: '',
  name: '',
});
const nodes: any = ref([])
const showEmpty = computed(() => isStandalone.value && !selectedHostId.value)
const checkedIds = ref([] as string[])

function actionButton(type: 'default' | 'error' | 'warning' | 'success' | 'info', iconCmp: any, tooltip: string, onClick: () => void) {
  return h(NTooltip, { trigger: 'hover' }, {
    trigger: () => h(NButton, { size: 'tiny', quaternary: true, type, onClick }, { icon: () => h(NIcon, null, { default: () => h(iconCmp) }) }),
    default: () => tooltip,
  })
}

async function doDelete(i: Image, _index: number, force: boolean) {
  try {
    await imageApi.delete(filter.node, i.id, "", force)
    // `_index` refers to the sorted view when a client-side sort is active,
    // so splice by identity instead.
    const idx = (state.data as Image[]).findIndex(x => x.id === i.id)
    if (idx >= 0) state.data.splice(idx, 1)
    message.success(t('buttons.delete'))
  } catch (e: any) {
    message.error(e?.message || String(e))
  }
}

function confirmDelete(i: Image, index: number) {
  dialog.warning({
    title: t('buttons.delete'),
    content: t('prompts.delete'),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: () => doDelete(i, index, false),
  })
}

function confirmForceDelete(i: Image, index: number) {
  const tagList = (i.tags || []).filter(x => x && x !== '<none>:<none>').join(', ') || i.id.substring(7, 19)
  dialog.error({
    title: t('buttons.force_delete') || 'Force delete',
    content: (t('prompts.force_delete') || 'This will remove the image from all repositories (untag every tag and delete image layers), even if referenced by containers. Proceed?') + '\n\n' + tagList,
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: () => doDelete(i, index, true),
  })
}
const columns = [
  { type: 'selection' as const },
  {
    title: t('fields.id'),
    key: "id",
    fixed: "left" as const,
    sorter: (a: Image, b: Image) => (a.id || '').localeCompare(b.id || ''),
    render: (i: Image) => {
      const idShort = i.id.substring(7, 19)
      const link = renderLink({ name: 'image_detail', params: { node: filter.node || '-', id: i.id } }, idShort)
      const unused = !i.containers || i.containers <= 0
      if (!unused) return link
      const badge = h(NTag, { size: 'small', type: 'warning', round: true, style: 'margin-left:6px' }, { default: () => t('fields.unused') || 'Unused' })
      return h('span', null, [link, badge])
    },
  },
  {
    title: t('fields.tags'),
    key: "tags",
    sorter: (a: Image, b: Image) => ((a.tags?.[0]) || '').localeCompare((b.tags?.[0]) || ''),
    render(i: Image) {
      if (i.tags) {
        return renderTags(i.tags?.map(t => {
          return { text: t, type: 'default' }
        }), true, 6)
      }
    },
  },
  {
    title: t('fields.size'),
    key: "size",
    sorter: (a: Image, b: Image) => (a.size || 0) - (b.size || 0),
    render(i: Image) {
      return formatSize(i.size)
    }
  },
  {
    title: t('fields.created_at'),
    key: "created",
    sorter: (a: Image, b: Image) => (a.created || '').localeCompare(b.created || ''),
  },
  {
    title: t('fields.actions'),
    key: "actions",
    width: 180,
    render(i: Image, index: number) {
      return h(NButtonGroup, null, {
        default: () => [
          actionButton('info', PricetagOutline, t('image.tag_action'), () => openTagDialog(i)),
          actionButton('success', CloudUploadOutline, t('image.push_action'), () => openPushDialog(i)),
          actionButton('error', TrashOutline, t('buttons.delete'), () => confirmDelete(i, index)),
          actionButton('error', FlashOffOutline, t('buttons.force_delete') || 'Force delete', () => confirmForceDelete(i, index)),
        ],
      })
    },
  },
];
const { state, pagination, fetchData, changePage, changePageSize, paginatedData, handleSorterChange, setSortColumns } = useDataTable(imageApi.search, filter, { remote: false, autoFetch: false })
setSortColumns(columns)

async function bulkDelete(force: boolean) {
  if (!checkedIds.value.length) return
  dialog.warning({
    title: force ? (t('buttons.force_delete') || 'Force delete') : t('buttons.delete'),
    content: force
      ? (t('prompts.force_delete') || 'This will remove these images from all repositories, even if referenced by containers. Proceed?')
      : t('prompts.delete'),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: async () => {
      const errors: string[] = []
      for (const id of [...checkedIds.value]) {
        try { await imageApi.delete(filter.node, id, "", force) }
        catch (e: any) { errors.push(`${id.substring(7,19)}: ${e?.message || e}`) }
      }
      checkedIds.value = []
      if (errors.length) message.error(errors.join('\n'))
      else message.success(t('buttons.delete'))
      fetchData()
    }
  })
}

async function prune() {
  window.dialog.warning({
    title: t('dialogs.prune_image.title'),
    content: t('dialogs.prune_image.body'),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: async () => {
      const r = await imageApi.prune(filter.node);
      window.message.info(t('texts.prune_image_success', {
        count: r.data?.count,
        size: formatSize(r.data?.size as number),
      }));
      fetchData();
    }
  })
}

watch(selectedHostId, (v) => {
  if (v) {
    filter.node = v
    // Immediate visual feedback while the refetch is in flight.
    state.data = [] as any
    checkedIds.value = []
    fetchData()
  }
})

// ------- Tag / Push dialogs (shared with View.vue via composable) -------
const {
  tagDialog,
  pushDialog,
  registryOptions,
  openTagDialog,
  openPushDialog,
  doTag,
  doPush,
} = useImageActions(() => filter.node, () => fetchData())

onMounted(async () => {
  if (isStandalone.value) {
    if (selectedHostId.value) {
      filter.node = selectedHostId.value
      fetchData()
    }
  } else {
    const r = await nodeApi.list(true)
    nodes.value = (r.data || []).map((n: any) => ({ label: n.name, value: n.id }))
    if (nodes.value.length) filter.node = nodes.value[0].value
    fetchData()
  }
})
</script>

<style scoped>
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
</style>