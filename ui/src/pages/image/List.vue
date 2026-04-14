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
        :row-key="row => row.name"
        size="small"
        :columns="columns"
        :data="state.data"
        :pagination="pagination"
        :loading="state.loading"
        @update:page="fetchData"
        @update-page-size="changePageSize"
        scroll-x="max-content"
      />
    </template>
  </n-space>
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
  useDialog,
  useMessage,
} from "naive-ui";
import {
  CloseOutline as CloseIcon,
  TrashOutline,
  FlashOffOutline,
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

function actionButton(type: 'default' | 'error' | 'warning' | 'success' | 'info', iconCmp: any, tooltip: string, onClick: () => void) {
  return h(NTooltip, { trigger: 'hover' }, {
    trigger: () => h(NButton, { size: 'tiny', quaternary: true, type, onClick }, { icon: () => h(NIcon, null, { default: () => h(iconCmp) }) }),
    default: () => tooltip,
  })
}

async function doDelete(i: Image, index: number, force: boolean) {
  try {
    await imageApi.delete(filter.node, i.id, "", force)
    state.data.splice(index, 1)
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
  {
    title: t('fields.id'),
    key: "id",
    fixed: "left" as const,
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
    render(i: Image) {
      return formatSize(i.size)
    }
  },
  {
    title: t('fields.created_at'),
    key: "created"
  },
  {
    title: t('fields.actions'),
    key: "actions",
    width: 120,
    render(i: Image, index: number) {
      return h(NButtonGroup, null, {
        default: () => [
          actionButton('error', TrashOutline, t('buttons.delete'), () => confirmDelete(i, index)),
          actionButton('error', FlashOffOutline, t('buttons.force_delete') || 'Force delete', () => confirmForceDelete(i, index)),
        ],
      })
    },
  },
];
const { state, pagination, fetchData, changePageSize } = useDataTable(imageApi.search, filter, false)

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
  if (v) { filter.node = v; fetchData() }
})

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