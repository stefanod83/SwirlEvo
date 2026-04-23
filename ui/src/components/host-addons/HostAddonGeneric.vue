<template>
  <x-panel
    :title="title"
    :subtitle="subtitle"
    divider="bottom"
    :collapsed="effectiveCollapsed"
  >
    <template #action>
      <n-space :size="12" align="center">
        <n-button
          v-if="isControlled"
          secondary
          strong
          size="small"
          style="min-width: 75px"
          @click="() => emit('toggle')"
        >{{ effectiveCollapsed ? t('buttons.expand') : t('buttons.collapse') }}</n-button>
        <n-space :size="6" align="center">
          <span style="font-size: 12px">{{ t('host_addon_traefik.enabled') }}</span>
          <n-switch :value="!!local.enabled" @update:value="setEnabled" />
        </n-space>
        <n-popconfirm
          v-if="hasAnyData"
          :show-icon="false"
          @positive-click="clearAll"
        >
          <template #trigger>
            <n-button size="small" quaternary type="error">
              {{ t('buttons.delete') }}
            </n-button>
          </template>
          {{ t('host_addon_generic.clear_confirm') }}
        </n-popconfirm>
      </n-space>
    </template>

    <n-alert v-if="!local.enabled" type="info" :show-icon="true" style="margin-bottom: 12px">
      {{ t('host_addon_generic.disabled_hint', { name: title }) }}
    </n-alert>

    <n-space vertical :size="16">
      <!-- (a) Pointer: which stack/container runs the addon ---------- -->
      <section class="addon-section">
        <h4 class="addon-section-title">{{ t('host_addon_generic.pointer') }}</h4>
        <n-form label-placement="top" :show-feedback="false">
          <n-form-item :label="t('host_addon_traefik.ref_stack')">
            <n-select
              v-model:value="local.stackId"
              :options="stackOptions"
              :placeholder="t('host_addon_traefik.ref_stack_placeholder')"
              filterable
              clearable
            />
          </n-form-item>
          <n-form-item :label="t('host_addon_traefik.container_name')">
            <n-input
              v-model:value="local.containerName"
              :placeholder="t('host_addon_traefik.container_name_placeholder')"
            />
          </n-form-item>
        </n-form>
      </section>

      <n-divider class="addon-divider" />

      <!-- (b) Defaults: free-form key/value ---------------------------- -->
      <section class="addon-section">
        <h4 class="addon-section-title">{{ t('host_addon_generic.defaults') }}</h4>
        <div class="muted" style="font-size: 12px; margin-bottom: 8px">
          {{ t('host_addon_generic.defaults_desc') }}
        </div>
        <n-table size="small" :bordered="true" :single-line="false">
          <thead>
            <tr>
              <th style="width: 40%">{{ t('fields.key') }}</th>
              <th>{{ t('fields.value') }}</th>
              <th style="width: 60px"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, idx) of defaultsRows" :key="'def-' + idx">
              <td><n-input size="small" v-model:value="row.k" /></td>
              <td><n-input size="small" v-model:value="row.v" /></td>
              <td>
                <n-button size="tiny" quaternary type="error" @click="removeDefault(idx)">
                  {{ t('buttons.delete') }}
                </n-button>
              </td>
            </tr>
            <tr v-if="!defaultsRows.length">
              <td colspan="3" style="text-align: center; padding: 8px" class="muted">
                {{ t('host_addon_generic.empty') }}
              </td>
            </tr>
          </tbody>
        </n-table>
        <n-button size="small" quaternary @click="addDefault" style="margin-top: 8px">
          <template #icon>
            <n-icon><add-icon /></n-icon>
          </template>
          {{ t('host_addon_generic.add_default') }}
        </n-button>
      </section>

      <n-divider class="addon-divider" />

      <!-- (c) Overrides ----------------------------------------------- -->
      <section class="addon-section">
        <h4 class="addon-section-title">{{ t('host_addon_traefik.overrides') }}</h4>
        <div class="muted" style="font-size: 12px; margin-bottom: 8px">
          {{ t('host_addon_traefik.overrides_desc') }}
        </div>
        <n-table size="small" :bordered="true" :single-line="false">
          <thead>
            <tr>
              <th style="width: 40%">{{ t('fields.key') }}</th>
              <th>{{ t('fields.value') }}</th>
              <th style="width: 60px"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, idx) of overridesRows" :key="'ovr-' + idx">
              <td><n-input size="small" v-model:value="row.k" /></td>
              <td><n-input size="small" v-model:value="row.v" /></td>
              <td>
                <n-button size="tiny" quaternary type="error" @click="removeOverride(idx)">
                  {{ t('buttons.delete') }}
                </n-button>
              </td>
            </tr>
            <tr v-if="!overridesRows.length">
              <td colspan="3" style="text-align: center; padding: 8px" class="muted">
                {{ t('host_addon_traefik.overrides_empty') }}
              </td>
            </tr>
          </tbody>
        </n-table>
        <n-button size="small" quaternary @click="addOverride" style="margin-top: 8px">
          <template #icon>
            <n-icon><add-icon /></n-icon>
          </template>
          {{ t('host_addon_traefik.add_override') }}
        </n-button>
      </section>

      <n-space justify="end">
        <n-button type="primary" :loading="saving" @click="save">
          <template #icon>
            <n-icon><save-icon /></n-icon>
          </template>
          {{ t('buttons.save') }}
        </n-button>
      </n-space>
    </n-space>
  </x-panel>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  NSpace, NButton, NIcon, NAlert, NForm, NFormItem, NInput, NSelect, NSwitch,
  NTable, NPopconfirm, NDivider,
  useMessage,
} from 'naive-ui'
import { AddOutline as AddIcon, SaveOutline as SaveIcon } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import XPanel from '@/components/Panel.vue'
import {
  getAddonExtract, saveAddonExtract, clearAddonExtract,
  type AddonConfigExtract, type GenericAddonExtract,
} from '@/api/host'
import composeStackApi from '@/api/compose_stack'

const props = defineProps<{
  hostId: string
  // Addon key on AddonConfigExtract / clearAddonExtract:
  // 'sablier' | 'watchtower' | 'backup' (and whatever future addons
  // we slot into this generic panel).
  addonKey: 'sablier' | 'watchtower' | 'backup'
  title: string
  subtitle: string
  collapsed?: boolean
}>()
const emit = defineEmits<{ (e: 'toggle'): void }>()

const { t } = useI18n()
const message = useMessage()

const isControlled = computed(() => props.collapsed !== undefined)
const effectiveCollapsed = computed(() =>
  isControlled.value ? !!props.collapsed : false,
)

const saving = ref(false)
const local = reactive<GenericAddonExtract>({
  enabled: false,
  stackId: '',
  containerName: '',
  defaults: {},
  overrides: {},
})
const defaultsRows = ref<{ k: string; v: string }[]>([])
const overridesRows = ref<{ k: string; v: string }[]>([])
const stackOptions = ref<{ label: string; value: string }[]>([])

async function load() {
  if (!props.hostId) return
  const [extractRes, stacksRes] = await Promise.allSettled([
    getAddonExtract(props.hostId),
    composeStackApi.search({ hostId: props.hostId, pageIndex: 1, pageSize: 1000 }),
  ])
  if (extractRes.status === 'fulfilled') {
    const sub = (extractRes.value.data as AddonConfigExtract)?.[props.addonKey] as GenericAddonExtract | undefined
    applyLocal(sub || {})
  }
  if (stacksRes.status === 'fulfilled') {
    const items = ((stacksRes.value.data as any)?.items || []) as { id: string; name: string; managed?: boolean }[]
    const managed = items
      .filter((s) => !!s.id && s.managed !== false)
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((s) => ({ label: s.name, value: s.id }))
    const unmanaged = items
      .filter((s) => !s.id || s.managed === false)
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((s) => ({
        label: `${s.name} · ${t('host_addon_traefik.ref_unmanaged')}`,
        value: `external:${s.name}`,
      }))
    stackOptions.value = [...managed, ...unmanaged]
  }
}

function applyLocal(v: GenericAddonExtract) {
  local.enabled = !!v.enabled
  local.stackId = v.stackId || ''
  local.containerName = v.containerName || ''
  local.defaults = { ...(v.defaults || {}) }
  local.overrides = { ...(v.overrides || {}) }
  defaultsRows.value = Object.keys(local.defaults!).map((k) => ({ k, v: local.defaults![k] }))
  overridesRows.value = Object.keys(local.overrides!).map((k) => ({ k, v: local.overrides![k] }))
}

const hasAnyData = computed(() =>
  !!(local.stackId || local.containerName
    || Object.keys(local.defaults || {}).length
    || Object.keys(local.overrides || {}).length),
)

function addDefault()     { defaultsRows.value.push({ k: '', v: '' }) }
function removeDefault(i: number) { defaultsRows.value.splice(i, 1) }
function addOverride()    { overridesRows.value.push({ k: '', v: '' }) }
function removeOverride(i: number) { overridesRows.value.splice(i, 1) }

async function setEnabled(v: boolean) {
  local.enabled = v
  await save()
}

async function clearAll() {
  try {
    await clearAddonExtract(props.hostId, props.addonKey)
    applyLocal({})
    message.success(t('host_addon_generic.clear_ok'))
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  }
}

async function save() {
  saving.value = true
  try {
    const defaults: Record<string, string> = {}
    for (const r of defaultsRows.value) if (r.k.trim()) defaults[r.k.trim()] = r.v
    const overrides: Record<string, string> = {}
    for (const r of overridesRows.value) if (r.k.trim()) overrides[r.k.trim()] = r.v
    const payload: GenericAddonExtract = {
      enabled: !!local.enabled,
      stackId: local.stackId || '',
      containerName: local.containerName || '',
      defaults,
      overrides,
    }
    await saveAddonExtract(props.hostId, { [props.addonKey]: payload } as AddonConfigExtract)
    message.success(t('host_addon_traefik.save_ok'))
    await load()
  } catch (e: any) {
    message.error(e?.response?.data?.info || e?.message || String(e))
  } finally {
    saving.value = false
  }
}

watch(() => props.hostId, () => { if (props.hostId) load() })
onMounted(() => { if (props.hostId) load() })
</script>

<style scoped>
.muted { color: var(--n-text-color-3, #999); }
.addon-section { display: block; }
.addon-section-title {
  font-size: 13px;
  font-weight: 600;
  margin: 0 0 10px 0;
  color: var(--n-text-color-2, #555);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.addon-divider { margin: 18px 0 !important; }
</style>
