<template>
  <x-page-header :subtitle="model.name">
    <template #action>
      <n-button secondary size="small" @click="$router.push({ name: 'registry_list' })">
        <template #icon>
          <n-icon><back-icon /></n-icon>
        </template>
        {{ t('buttons.return') }}
      </n-button>
      <n-button secondary size="small" @click="fetchData" :loading="loading">
        <template #icon>
          <n-icon><refresh-icon /></n-icon>
        </template>
        {{ t('buttons.refresh') }}
      </n-button>
      <n-button
        secondary
        size="small"
        @click="$router.push({ name: 'registry_edit', params: { id: model.id } })"
      >{{ t('buttons.edit') }}</n-button>
    </template>
  </x-page-header>

  <n-tabs type="line" default-value="detail" class="page-body">
    <n-tab-pane name="detail" :tab="t('fields.detail')">
      <x-description label-placement="left" label-align="right" :label-width="110">
        <x-description-item :label="t('fields.id')">{{ model.id }}</x-description-item>
        <x-description-item :label="t('fields.name')">{{ model.name }}</x-description-item>
        <x-description-item :label="t('fields.url')">{{ model.url }}</x-description-item>
        <x-description-item :label="t('fields.login_name')">{{ model.username }}</x-description-item>
        <x-description-item :label="t('registry.skip_tls_verify')">
          {{ model.skipTlsVerify ? t('enums.yes') : t('enums.no') }}
        </x-description-item>
        <x-description-item :label="t('fields.created_by')">
          <x-anchor
            :url="{ name: 'user_detail', params: { id: model.createdBy?.id } }"
            v-if="model.createdBy?.id"
          >{{ model.createdBy?.name }}</x-anchor>
        </x-description-item>
        <x-description-item :label="t('fields.created_at')">
          <n-time :time="model.createdAt" format="y-MM-dd HH:mm:ss" />
        </x-description-item>
        <x-description-item :label="t('fields.updated_by')">
          <x-anchor
            :url="{ name: 'user_detail', params: { id: model.updatedBy?.id } }"
            v-if="model.updatedBy?.id"
          >{{ model.updatedBy?.name }}</x-anchor>
        </x-description-item>
        <x-description-item :label="t('fields.updated_at')">
          <n-time :time="model.updatedAt" format="y-MM-dd HH:mm:ss" />
        </x-description-item>
      </x-description>
    </n-tab-pane>

    <n-tab-pane name="repos" :tab="t('registry.repositories')">
      <n-space vertical :size="12">
        <n-space :size="8" align="center">
          <n-input
            size="small"
            v-model:value="repoFilter"
            :placeholder="t('registry.filter_repo')"
            clearable
            style="min-width: 240px;"
          />
          <n-button size="small" secondary :loading="loadingRepos" @click="loadRepos(true)">
            <template #icon>
              <n-icon><refresh-icon /></n-icon>
            </template>
            {{ t('buttons.refresh') }}
          </n-button>
          <n-button
            v-if="next"
            size="small"
            secondary
            :loading="loadingRepos"
            @click="loadRepos(false)"
          >{{ t('registry.load_more') }}</n-button>
          <span v-if="repoError" class="error">{{ repoError }}</span>
        </n-space>

        <n-empty
          v-if="!loadingRepos && !repos.length"
          :description="t('registry.no_repos')"
        />

        <n-table v-else size="small" :bordered="true" :single-line="false">
          <thead>
            <tr>
              <th>{{ t('registry.repository') }}</th>
              <th style="width: 200px;">{{ t('fields.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r of filteredRepos" :key="r">
              <td><code class="mono">{{ r }}</code></td>
              <td>
                <n-button size="tiny" quaternary type="info" @click="openTags(r)">
                  {{ t('registry.show_tags') }}
                </n-button>
              </td>
            </tr>
          </tbody>
        </n-table>
      </n-space>
    </n-tab-pane>
  </n-tabs>

  <!-- Tags drawer -->
  <n-drawer v-model:show="tagsOpen" :width="420" placement="right">
    <n-drawer-content
      :title="t('registry.tags_for', { repo: tagsRepo })"
      closable
    >
      <n-spin :show="loadingTags">
        <n-space vertical :size="6">
          <n-tag v-for="t of tags" :key="t" size="small" type="info">{{ t }}</n-tag>
          <span v-if="!tags.length && !loadingTags" class="muted">
            {{ t('registry.no_tags') }}
          </span>
          <n-alert v-if="tagsError" type="error">{{ tagsError }}</n-alert>
        </n-space>
      </n-spin>
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  NButton, NSpace, NIcon, NTime, NTabs, NTabPane, NTable, NInput, NEmpty,
  NDrawer, NDrawerContent, NTag, NAlert, NSpin,
} from "naive-ui";
import {
  ArrowBackCircleOutline as BackIcon,
  RefreshOutline as RefreshIcon,
} from "@vicons/ionicons5";
import XPageHeader from "@/components/PageHeader.vue";
import XAnchor from "@/components/Anchor.vue";
import { XDescription, XDescriptionItem } from "@/components/description";
import registryApi from "@/api/registry";
import type { Registry } from "@/api/registry";
import { useRoute } from "vue-router";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute()
const model = ref({} as Registry)
const loading = ref(false)

const repos = ref<string[]>([])
const next = ref('')
const loadingRepos = ref(false)
const repoError = ref('')
const repoFilter = ref('')

const tagsOpen = ref(false)
const tagsRepo = ref('')
const tags = ref<string[]>([])
const loadingTags = ref(false)
const tagsError = ref('')

const filteredRepos = computed(() => {
  const q = repoFilter.value.trim().toLowerCase()
  if (!q) return repos.value
  return repos.value.filter(r => r.toLowerCase().includes(q))
})

async function loadRepos(reset: boolean) {
  if (!model.value.id) return
  loadingRepos.value = true
  repoError.value = ''
  try {
    const r = await registryApi.browse(model.value.id, 100, reset ? '' : next.value)
    const data = r.data || { repos: [], next: '' }
    if (reset) repos.value = data.repos || []
    else repos.value = repos.value.concat(data.repos || [])
    next.value = data.next || ''
  } catch (e: any) {
    repoError.value = e?.message || String(e)
  } finally {
    loadingRepos.value = false
  }
}

async function openTags(repo: string) {
  tagsRepo.value = repo
  tags.value = []
  tagsError.value = ''
  tagsOpen.value = true
  loadingTags.value = true
  try {
    const r = await registryApi.tags(model.value.id, repo)
    tags.value = r.data || []
  } catch (e: any) {
    tagsError.value = e?.message || String(e)
  } finally {
    loadingTags.value = false
  }
}

async function fetchData() {
  loading.value = true
  try {
    const r = await registryApi.find(route.params.id as string)
    model.value = r.data as Registry
    loadRepos(true)
  } finally {
    loading.value = false
  }
}

onMounted(fetchData)
</script>

<style scoped>
.muted { color: var(--n-text-color-3, #999); }
.error { color: var(--n-color-target, #e88080); font-size: 12px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
</style>
