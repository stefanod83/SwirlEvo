<template>
  <x-page-header :subtitle="image.id">
    <template #action>
      <n-space :size="8">
        <n-button secondary size="small" @click="$router.push({ name: 'image_list' })">
          <template #icon>
            <n-icon>
              <back-icon />
            </n-icon>
          </template>
          {{ t('buttons.return') }}
        </n-button>
        <n-button secondary size="small" @click="fetchData" :loading="loading">
          <template #icon>
            <n-icon>
              <refresh-outline />
            </n-icon>
          </template>
          {{ t('buttons.refresh') || 'Refresh' }}
        </n-button>
        <n-button
          secondary
          size="small"
          v-if="store.getters.allow('image.edit')"
          @click="openTagDialog(image)"
        >
          <template #icon>
            <n-icon>
              <pricetag-outline />
            </n-icon>
          </template>
          {{ t('image.tag_action') }}
        </n-button>
        <n-button
          secondary
          size="small"
          v-if="store.getters.allow('image.push')"
          @click="openPushDialog(image)"
        >
          <template #icon>
            <n-icon>
              <cloud-upload-outline />
            </n-icon>
          </template>
          {{ t('image.push_action') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>
  <div class="page-body">
    <n-tabs type="line" style="margin-top: -12px">
      <n-tab-pane name="detail" :tab="t('fields.detail')">
        <n-space vertical :size="16">
          <x-description label-placement="left" label-align="right" :label-width="110">
            <x-description-item :label="t('fields.id')" :span="2">{{ image.id }}</x-description-item>
            <x-description-item
              :label="t('fields.tags')"
              :span="2"
              v-if="image.tags && image.tags.length"
            >
              <n-space :size="4">
                <n-tag round size="small" type="default" v-for="tag in image.tags">{{ tag }}</n-tag>
              </n-space>
            </x-description-item>
            <x-description-item :label="t('fields.created_at')" :span="2">{{ image.created }}</x-description-item>
            <x-description-item :label="t('fields.size')">{{ formatSize(image.size) }}</x-description-item>
            <x-description-item :label="t('fields.platform')">{{ image.os + "/" + image.arch }}</x-description-item>
            <x-description-item
              :label="t('fields.docker_version')"
              v-if="image.dockerVersion"
              :span="2"
            >{{ image.dockerVersion }}</x-description-item>
            <x-description-item
              :label="t('fields.graph_driver')"
              v-if="image.graphDriver?.name"
            >{{ image.graphDriver?.name }}</x-description-item>
            <x-description-item
              :label="t('fields.root_fs')"
              v-if="image.rootFS?.type"
            >{{ image.rootFS?.type }}</x-description-item>
            <x-description-item
              :label="t('fields.comment')"
              v-if="image.comment"
              :span="2"
            >{{ image.comment }}</x-description-item>
          </x-description>
          <x-panel :title="t('fields.layers')" v-if="image.histories && image.histories.length">
            <n-data-table
              remote
              size="small"
              :columns="columns"
              :data="image.histories"
              scroll-x="max-content"
            />
          </x-panel>
        </n-space>
      </n-tab-pane>
      <n-tab-pane name="raw" :tab="t('fields.raw')">
        <x-code :code="raw" language="json" />
      </n-tab-pane>
    </n-tabs>
  </div>

  <!-- Tag dialog (shared with List.vue via useImageActions) -->
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

  <!-- Push dialog (shared with List.vue via useImageActions) -->
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
import { onMounted, ref } from "vue";
import {
  NButton,
  NTag,
  NSpace,
  NIcon,
  NDataTable,
  NTabs,
  NTabPane,
  NModal,
  NInput,
  NSelect,
  NFormItem,
  NAlert,
} from "naive-ui";
import {
  ArrowBackCircleOutline as BackIcon,
  RefreshOutline,
  PricetagOutline,
  CloudUploadOutline,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XCode from "@/components/Code.vue";
import XPanel from "@/components/Panel.vue";
import { XDescription, XDescriptionItem } from "@/components/description";
import imageApi from "@/api/image";
import type { Image } from "@/api/image";
import { useRoute } from "vue-router";
import { useStore } from "vuex";
import { formatSize, renderTags } from "@/utils/render";
import { useI18n } from 'vue-i18n'
import { useImageActions } from "./useImageActions";

const { t } = useI18n()
const route = useRoute();
const store = useStore();
const image = ref({} as Image);
const raw = ref('');
const loading = ref(false);
const node = route.params.node as string || '';

const {
  tagDialog,
  pushDialog,
  registryOptions,
  openTagDialog,
  openPushDialog,
  doTag,
  doPush,
} = useImageActions(() => node, () => fetchData())

const columns = [
  {
    title: t('fields.sn'),
    key: "no",
    width: 45,
    fixed: "left" as const,
    render: (h: any, i: number) => i + 1,
  },
  {
    title: t('fields.instruction'),
    key: "createdBy",
    width: 500,
  },
  {
    title: t('fields.tags'),
    key: "image",
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
    width: 90,
    render(i: Image) {
      return formatSize(i.size)
    }
  },
  {
    title: t('fields.comment'),
    key: "comment",
  },
  {
    title: t('fields.created_at'),
    key: "createdAt",
    width: 150,
  },
];

async function fetchData() {
  loading.value = true;
  try {
    const id = route.params.id as string;
    let r = await imageApi.find(node, id);
    raw.value = r.data?.raw as string;
    image.value = r.data?.image as Image;
    image.value.histories && image.value.histories.reverse();
  } finally {
    loading.value = false;
  }
}

onMounted(fetchData);
</script>

<style scoped>
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
</style>
