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
      <n-button secondary size="small" @click="$router.push({name: 'volume_new', params: {node: filter.node || '-'}})">
        <template #icon>
          <n-icon>
            <add-icon />
          </n-icon>
        </template>
        {{ t('buttons.new') }}
      </n-button>
    </template>
  </x-page-header>
  <n-space class="page-body" vertical :size="12">
    <x-empty-host-prompt v-if="showEmpty" :resource="t('objects.volume', 2)" />
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
  NDataTable,
  NInput,
  NIcon,
  NSelect,
  NTag,
} from "naive-ui";
import { AddOutline as AddIcon, CloseOutline as CloseIcon } from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import volumeApi from "@/api/volume";
import type { Volume } from "@/api/volume";
import nodeApi from "@/api/node";
import { useStore } from "vuex";
import XEmptyHostPrompt from "@/components/EmptyHostPrompt.vue";
import { computed, watch } from "vue";
import { useDataTable } from "@/utils/data-table";
import { formatSize, renderButton, renderLink, renderTag } from "@/utils/render";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const store = useStore()
const isStandalone = computed(() => store.state.mode === 'standalone')
const selectedHostId = computed(() => store.state.selectedHostId as string | null)
const filter = reactive({
  node: '',
  name: '',
});
const nodes: any = ref([])
const showEmpty = computed(() => isStandalone.value && !selectedHostId.value)
const columns = [
  {
    title: t('fields.name'),
    key: "name",
    fixed: "left" as const,
    render: (v: Volume) => {
      const link = renderLink({ name: 'volume_detail', params: { node: filter.node || '-', name: v.name } }, v.name)
      const unused = !v.refCount || v.refCount <= 0
      if (!unused) return link
      const badge = h(NTag, { size: 'small', type: 'warning', round: true, style: 'margin-left:6px' }, { default: () => t('fields.unused') || 'Unused' })
      return h('span', null, [link, badge])
    },
  },
  {
    title: t('fields.driver'),
    key: "driver",
    render(v: Volume) {
      return renderTag(v.driver)
    }
  },
  {
    title: t('fields.scope'),
    key: "scope",
    render(v: Volume) {
      return renderTag(v.scope)
    }
  },
  {
    title: t('fields.mount_point'),
    key: "mountPoint"
  },
  {
    title: t('fields.created_at'),
    key: "createdAt"
  },
  {
    title: t('fields.actions'),
    key: "actions",
    render(v: Volume, index: number) {
      return renderButton('error', t('buttons.delete'), () => remove(v.name, index), t('prompts.delete'))
    },
  },
];
const { state, pagination, fetchData, changePageSize } = useDataTable(volumeApi.search, filter, false)

async function remove(name: string, index: number) {
  await volumeApi.delete(filter.node, name);
  state.data.splice(index, 1)
}

async function prune() {
  window.dialog.warning({
    title: t('dialogs.prune_volume.title'),
    content: t('dialogs.prune_volume.body'),
    positiveText: t('buttons.confirm'),
    negativeText: t('buttons.cancel'),
    onPositiveClick: async () => {
      const r = await volumeApi.prune(filter.node);
      window.message.info(t('texts.prune_volume_success', {
        count: r.data?.count,
        size: formatSize(r.data?.size as number)
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