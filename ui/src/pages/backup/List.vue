<template>
  <x-page-header :subtitle="t('texts.records', { total: backups.length }, backups.length)">
    <template #action>
      <n-space size="small">
        <n-upload
          :show-file-list="false"
          accept=".swb,.enc"
          :custom-request="onUploadFile"
        >
          <n-button secondary size="small">
            <template #icon>
              <n-icon><cloud-upload-icon /></n-icon>
            </template>
            {{ t('backup.restore_from_file') }}
          </n-button>
        </n-upload>
        <n-button
          secondary
          size="small"
          type="primary"
          :loading="creating"
          :disabled="!status.keyConfigured"
          @click="onCreate"
        >
          <template #icon>
            <n-icon><add-icon /></n-icon>
          </template>
          {{ t('backup.create') }}
        </n-button>
      </n-space>
    </template>
  </x-page-header>

  <n-space class="page-body" vertical :size="12">
    <n-alert v-if="!status.keyConfigured" type="warning" :title="t('backup.key_missing_title')">
      <div>{{ t('backup.key_missing_body') }}</div>
      <div v-if="status.keyError" style="margin-top: 6px;">
        <strong>{{ t('backup.key_lookup_error') }}:</strong> <code>{{ status.keyError }}</code>
      </div>
    </n-alert>

    <x-panel
      :title="t('backup.schedules')"
      :subtitle="t('backup.schedules_hint')"
      divider="bottom"
      :collapsed="!schedulesOpen"
    >
      <template #action>
        <n-button secondary size="small" @click="schedulesOpen = !schedulesOpen">
          {{ schedulesOpen ? t('buttons.collapse') : t('buttons.expand') }}
        </n-button>
      </template>

      <n-grid :cols="3" :x-gap="12" :y-gap="12" responsive="screen" item-responsive>
        <n-gi v-for="kind in (['daily','weekly','monthly'] as const)" :key="kind" :span="1" span-sm="3" span-md="1">
          <n-card size="small" :title="t('backup.schedule_' + kind)">
            <n-form size="small" label-placement="left" label-width="auto">
              <n-form-item :label="t('fields.enabled')">
                <n-switch v-model:value="schedulesForm[kind].enabled" />
              </n-form-item>
              <n-form-item v-if="kind === 'daily'" :label="t('backup.days_of_week')">
                <n-checkbox-group v-model:value="schedulesForm[kind].daysArr">
                  <n-space :size="4">
                    <n-checkbox v-for="d in 7" :key="d - 1" :value="d - 1" :label="weekdayLabel(d - 1)" />
                  </n-space>
                </n-checkbox-group>
              </n-form-item>
              <n-form-item v-if="kind === 'weekly'" :label="t('backup.day_of_week')">
                <n-select
                  v-model:value="schedulesForm[kind].singleDay"
                  :options="weekdayOptions"
                />
              </n-form-item>
              <n-form-item v-if="kind === 'monthly'" :label="t('backup.day_of_month')">
                <n-input-number
                  v-model:value="schedulesForm[kind].monthDay"
                  :min="1"
                  :max="28"
                />
              </n-form-item>
              <n-form-item :label="t('backup.time')">
                <n-time-picker
                  v-model:formatted-value="schedulesForm[kind].time"
                  format="HH:mm"
                  value-format="HH:mm"
                />
              </n-form-item>
              <n-form-item :label="t('backup.retention')">
                <n-input-number v-model:value="schedulesForm[kind].retention" :min="0" />
              </n-form-item>
              <div v-if="schedulesForm[kind].lastRunAt" style="font-size: 12px; opacity: 0.6; margin-bottom: 8px;">
                {{ t('backup.last_run_at') }}:
                <n-time :time="new Date(schedulesForm[kind].lastRunAt!)" format="y-MM-dd HH:mm:ss" />
              </div>
              <n-space size="small">
                <n-button size="tiny" type="primary" @click="saveSchedule(kind)">{{ t('buttons.save') }}</n-button>
                <n-popconfirm :show-icon="false" @positive-click="deleteSchedule(kind)">
                  <template #trigger>
                    <n-button size="tiny" type="error" ghost>{{ t('buttons.delete') }}</n-button>
                  </template>
                  {{ t('prompts.delete') }}
                </n-popconfirm>
              </n-space>
            </n-form>
          </n-card>
        </n-gi>
      </n-grid>
    </x-panel>

    <n-alert
      v-if="keySummary && keySummary.incompatible > 0"
      type="error"
    >
      <n-space :size="12" align="center">
        <span>{{ t('backup.key_summary_banner', { incompatible: keySummary.incompatible }) }}</span>
        <n-button size="tiny" type="error" @click="refreshKeyStatus">
          {{ t('backup.key_summary_refresh') }}
        </n-button>
      </n-space>
    </n-alert>

    <n-table size="small" :bordered="true" :single-line="false">
      <thead>
        <tr>
          <th>{{ t('fields.name') }}</th>
          <th>{{ t('backup.source') }}</th>
          <th>{{ t('backup.key_status') }}</th>
          <th>{{ t('backup.size') }}</th>
          <th>{{ t('fields.created_at') }}</th>
          <th>{{ t('fields.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="backups.length === 0">
          <td colspan="6" style="text-align:center; opacity: 0.6;">{{ t('backup.empty') }}</td>
        </tr>
        <tr v-for="(r, index) of backups" :key="r.id">
          <td>
            <span>{{ r.name }}</span>
          </td>
          <td>
            <n-tag size="small" :type="sourceColor(r.source)">{{ t('backup.source_' + r.source) }}</n-tag>
          </td>
          <td>
            <n-tooltip v-if="keyBadge(r)" trigger="hover">
              <template #trigger>
                <n-tag size="small" :type="keyBadge(r)!.type as any">
                  {{ keyBadge(r)!.label }}
                </n-tag>
              </template>
              {{ keyBadge(r)!.tooltip }}
            </n-tooltip>
          </td>
          <td>{{ formatSize(r.size) }}</td>
          <td>
            <n-time :time="new Date(r.createdAt)" format="y-MM-dd HH:mm:ss" />
          </td>
          <td>
            <n-space size="small">
              <n-button size="tiny" quaternary @click="openDownload(r)">
                {{ t('buttons.download') }}
              </n-button>
              <n-button size="tiny" quaternary type="warning" @click="openRestore(r)">
                {{ t('backup.restore') }}
              </n-button>
              <n-button
                v-if="r.keyStatus === 'unverified'"
                size="tiny"
                quaternary
                type="info"
                @click="doVerify(r, index)"
              >
                {{ t('backup.verify') }}
              </n-button>
              <n-button
                v-if="r.keyStatus === 'incompatible' || r.keyStatus === 'unverified'"
                size="tiny"
                quaternary
                type="warning"
                @click="openRecover(r)"
              >
                {{ t('backup.recover') }}
              </n-button>
              <n-popconfirm :show-icon="false" @positive-click="deleteBackup(r.id, index)">
                <template #trigger>
                  <n-button size="tiny" quaternary type="error">{{ t('buttons.delete') }}</n-button>
                </template>
                {{ t('prompts.delete') }}
              </n-popconfirm>
            </n-space>
          </td>
        </tr>
      </tbody>
    </n-table>
  </n-space>

  <n-modal
    v-model:show="downloadDialog.show"
    preset="dialog"
    :title="t('backup.download_title')"
    :positive-text="t('buttons.download')"
    :negative-text="t('buttons.cancel')"
    @positive-click="doDownload"
  >
    <n-space vertical :size="8">
      <n-alert type="warning">{{ t('backup.download_warning') }}</n-alert>
      <n-radio-group v-model:value="downloadDialog.mode">
        <n-space vertical>
          <n-radio value="raw">{{ t('backup.download_mode_raw') }}</n-radio>
          <n-radio value="portable">{{ t('backup.download_mode_portable') }}</n-radio>
        </n-space>
      </n-radio-group>
      <n-input
        v-if="downloadDialog.mode === 'portable'"
        v-model:value="downloadDialog.password"
        type="password"
        :placeholder="t('backup.passphrase')"
      />
    </n-space>
  </n-modal>

  <n-modal
    v-model:show="restoreDialog.show"
    preset="dialog"
    style="width: 560px"
    :title="t('backup.restore_title')"
    :positive-text="restoreDialog.step === 3 ? t('backup.restore_confirm') : t('buttons.next')"
    :negative-text="restoreDialog.step === 1 ? t('buttons.cancel') : t('buttons.prev')"
    @positive-click="restoreNext"
    @negative-click="restorePrev"
  >
    <div v-if="restoreDialog.step === 1">
      <n-alert type="error" :title="t('backup.restore_warning_title')">
        {{ t('backup.restore_warning_body') }}
      </n-alert>
    </div>
    <div v-else-if="restoreDialog.step === 2">
      <div style="margin-bottom: 10px; opacity: 0.7;">{{ t('backup.restore_components_hint') }}</div>
      <n-checkbox-group v-model:value="restoreDialog.components">
        <n-space vertical>
          <n-checkbox v-for="c in allComponents" :key="c" :value="c">
            {{ t('backup.component_' + c) }}
            <span v-if="restoreDialog.stats[c] !== undefined" style="opacity: 0.6;">
              ({{ restoreDialog.stats[c] }})
            </span>
          </n-checkbox>
        </n-space>
      </n-checkbox-group>
    </div>
    <div v-else-if="restoreDialog.step === 3">
      <n-alert type="warning">
        {{ t('backup.restore_final_confirm', { count: restoreDialog.components.length }) }}
      </n-alert>
    </div>
  </n-modal>

  <n-modal
    v-model:show="uploadDialog.show"
    preset="dialog"
    style="width: 560px"
    :title="t('backup.upload_title')"
    :positive-text="uploadDialog.step === 3 ? t('backup.restore_confirm') : t('buttons.next')"
    :negative-text="uploadDialog.step === 1 ? t('buttons.cancel') : t('buttons.prev')"
    @positive-click="uploadNext"
    @negative-click="uploadPrev"
  >
    <div v-if="uploadDialog.step === 1">
      <p>{{ t('backup.upload_file', { name: uploadDialog.file?.name }) }}</p>
      <n-input
        v-if="uploadDialog.needsPassword"
        v-model:value="uploadDialog.password"
        type="password"
        :placeholder="t('backup.passphrase')"
      />
    </div>
    <div v-else-if="uploadDialog.step === 2">
      <n-checkbox-group v-model:value="uploadDialog.components">
        <n-space vertical>
          <n-checkbox v-for="c in allComponents" :key="c" :value="c">
            {{ t('backup.component_' + c) }}
            <span v-if="uploadDialog.stats[c] !== undefined" style="opacity: 0.6;">
              ({{ uploadDialog.stats[c] }})
            </span>
          </n-checkbox>
        </n-space>
      </n-checkbox-group>
    </div>
    <div v-else-if="uploadDialog.step === 3">
      <n-alert type="warning">
        {{ t('backup.restore_final_confirm', { count: uploadDialog.components.length }) }}
      </n-alert>
    </div>
  </n-modal>

  <n-modal
    v-model:show="recoverDialog.show"
    preset="dialog"
    style="width: 480px"
    :title="t('backup.recover_title')"
    :positive-text="t('backup.recover')"
    :negative-text="t('buttons.cancel')"
    :loading="recoverDialog.loading"
    @positive-click="doRecover"
  >
    <n-space vertical :size="12">
      <n-alert type="info">{{ t('backup.recover_hint') }}</n-alert>
      <n-input
        v-model:value="recoverDialog.oldPassphrase"
        type="password"
        :placeholder="t('backup.recover_passphrase')"
        show-password-on="click"
        @keyup.enter="doRecover"
      />
    </n-space>
  </n-modal>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import {
  NSpace, NButton, NTable, NTag, NTime, NPopconfirm, NIcon, NAlert,
  NCard, NForm, NFormItem, NSwitch, NInput, NInputNumber, NTimePicker,
  NSelect, NCheckbox, NCheckboxGroup, NRadio, NRadioGroup, NModal,
  NGrid, NGi, NUpload, NTooltip,
} from 'naive-ui'
import type { UploadCustomRequestOptions } from 'naive-ui'
import { AddOutline as AddIcon, CloudUploadOutline as CloudUploadIcon } from '@vicons/ionicons5'
import XPageHeader from '@/components/PageHeader.vue'
import XPanel from '@/components/Panel.vue'
import backupApi from '@/api/backup'
import type { Backup, BackupSchedule, BackupStatus, BackupKeyStatusSummary } from '@/api/backup'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const allComponents = [
  'settings', 'roles', 'users', 'registries',
  'stacks', 'composeStacks', 'hosts', 'charts', 'events',
]

const status = ref<BackupStatus>({ keyConfigured: true })
const backups = ref<Backup[]>([])
const creating = ref(false)
const schedulesOpen = ref(false)
const keySummary = ref<BackupKeyStatusSummary | null>(null)

type ScheduleKind = 'daily' | 'weekly' | 'monthly'

interface ScheduleForm {
  id: ScheduleKind
  enabled: boolean
  daysArr: number[]  // for daily
  singleDay: number  // for weekly
  monthDay: number   // for monthly
  time: string
  retention: number
  lastRunAt: string | null
}

const schedulesForm = reactive<Record<ScheduleKind, ScheduleForm>>({
  daily:   defaultForm('daily'),
  weekly:  defaultForm('weekly'),
  monthly: defaultForm('monthly'),
})

function defaultForm(kind: ScheduleKind): ScheduleForm {
  return {
    id: kind,
    enabled: false,
    daysArr: [1, 2, 3, 4, 5],
    singleDay: 1,
    monthDay: 1,
    time: '02:00',
    retention: 7,
    lastRunAt: null,
  }
}

function weekdayLabel(d: number): string {
  return t(`backup.weekday_${d}`)
}

const weekdayOptions = Array.from({ length: 7 }, (_, i) => ({
  label: t(`backup.weekday_${i}`),
  value: i,
}))

async function refresh() {
  const st = await backupApi.status()
  status.value = st.data || { keyConfigured: false }

  const r = await backupApi.search()
  backups.value = r.data || []

  const s = await backupApi.schedules()
  for (const row of (s.data || [])) loadSchedule(row)

  // The key-status summary drives the page-level banner. Best-effort:
  // if the call fails (e.g. API not yet rolled out) the rest of the page
  // still renders.
  await refreshKeyStatus()
}

async function refreshKeyStatus() {
  try {
    const r = await backupApi.keyStatus()
    keySummary.value = r.data || null
  } catch {
    keySummary.value = null
  }
}

// keyBadge maps a backup's KeyStatus to a Naive UI tag config. Returns
// null when no badge should be rendered (compatible / unknown).
function keyBadge(r: Backup): { label: string; type: string; tooltip: string } | null {
  switch (r.keyStatus) {
    case 'incompatible':
      return { label: t('backup.key_incompatible'), type: 'error', tooltip: t('backup.key_incompatible_tooltip') }
    case 'unverified':
      return { label: t('backup.key_unverified'), type: 'warning', tooltip: t('backup.key_unverified_tooltip') }
    case 'missing':
      return { label: t('backup.key_missing_file'), type: 'default', tooltip: t('backup.recover_failed_missing_file') }
    case 'unknown':
      return { label: t('backup.key_unknown'), type: 'default', tooltip: t('backup.key_check_skipped') }
    default:
      return null
  }
}

async function doVerify(r: Backup, index: number) {
  try {
    const resp = await backupApi.verify(r.id)
    if (resp.data) backups.value[index] = resp.data
    if (resp.data?.keyStatus === 'compatible') {
      window.message?.success?.(t('backup.verify_done'))
    } else {
      window.message?.warning?.(t('backup.verify_failed'))
    }
    await refreshKeyStatus()
  } catch (e: any) {
    window.message?.error?.(e?.message || String(e))
  }
}

function loadSchedule(row: BackupSchedule) {
  const form = schedulesForm[row.id]
  form.enabled = row.enabled
  form.time = row.time || '02:00'
  form.retention = row.retention ?? 0
  form.lastRunAt = row.lastRunAt ?? null
  if (row.id === 'daily') {
    form.daysArr = (row.dayConfig || '').split(',').map(x => parseInt(x.trim(), 10)).filter(x => !isNaN(x))
  } else if (row.id === 'weekly') {
    form.singleDay = parseInt(row.dayConfig || '1', 10)
  } else if (row.id === 'monthly') {
    form.monthDay = parseInt(row.dayConfig || '1', 10)
  }
}

function scheduleToPayload(form: ScheduleForm): BackupSchedule {
  let dayConfig = ''
  if (form.id === 'daily') dayConfig = form.daysArr.join(',')
  else if (form.id === 'weekly') dayConfig = String(form.singleDay)
  else dayConfig = String(form.monthDay)
  return {
    id: form.id,
    enabled: form.enabled,
    dayConfig,
    time: form.time,
    retention: form.retention,
  }
}

async function saveSchedule(kind: ScheduleKind) {
  await backupApi.saveSchedule(scheduleToPayload(schedulesForm[kind]))
  window.message?.success?.(t('backup.schedule_saved'))
  await refresh()
}

async function deleteSchedule(kind: ScheduleKind) {
  await backupApi.deleteSchedule(kind)
  Object.assign(schedulesForm[kind], defaultForm(kind))
  await refresh()
}

async function onCreate() {
  creating.value = true
  try {
    await backupApi.create('manual')
    await refresh()
  } finally {
    creating.value = false
  }
}

async function deleteBackup(id: string, index: number) {
  await backupApi.delete(id)
  backups.value.splice(index, 1)
}

function sourceColor(source: string): any {
  switch (source) {
    case 'manual':  return 'primary'
    case 'daily':   return 'success'
    case 'weekly':  return 'info'
    case 'monthly': return 'warning'
    default:        return 'default'
  }
}

function formatSize(bytes: number): string {
  if (!bytes || bytes < 0) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}

// ------- Download dialog -------

const downloadDialog = reactive({
  show: false,
  id: '',
  mode: 'raw' as 'raw' | 'portable',
  password: '',
  filename: '',
})

function openDownload(r: Backup) {
  downloadDialog.id = r.id
  downloadDialog.mode = 'raw'
  downloadDialog.password = ''
  downloadDialog.filename = r.name
  downloadDialog.show = true
}

async function doDownload() {
  if (downloadDialog.mode === 'portable' && !downloadDialog.password) {
    window.message?.error?.(t('backup.passphrase_required'))
    return false
  }
  const blob = await backupApi.download(downloadDialog.id, downloadDialog.mode, downloadDialog.password)
  const url = URL.createObjectURL(blob as Blob)
  const a = document.createElement('a')
  a.href = url
  a.download = downloadDialog.filename + (downloadDialog.mode === 'portable' ? '.enc' : '.swb')
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
  return true
}

// ------- Restore dialog (stored backup) -------

const restoreDialog = reactive<{
  show: boolean
  step: number
  id: string
  name: string
  components: string[]
  stats: { [key: string]: number }
}>({
  show: false,
  step: 1,
  id: '',
  name: '',
  components: [],
  stats: {},
})

async function openRestore(r: Backup) {
  restoreDialog.show = true
  restoreDialog.step = 1
  restoreDialog.id = r.id
  restoreDialog.name = r.name
  restoreDialog.stats = r.stats || {}
  restoreDialog.components = allComponents.filter(c => c !== 'events')
}

async function restoreNext() {
  if (restoreDialog.step < 3) {
    restoreDialog.step++
    return false // keep dialog open
  }
  await backupApi.restore(restoreDialog.id, restoreDialog.components)
  restoreDialog.show = false
  window.message?.success?.(t('backup.restore_done'))
  return true
}

function restorePrev() {
  if (restoreDialog.step > 1) {
    restoreDialog.step--
    return false
  }
  return true
}

// ------- Restore from file -------

const uploadDialog = reactive<{
  show: boolean
  step: number
  file: File | null
  password: string
  needsPassword: boolean
  components: string[]
  stats: { [key: string]: number }
}>({
  show: false,
  step: 1,
  file: null,
  password: '',
  needsPassword: false,
  components: [],
  stats: {},
})

function onUploadFile(opts: UploadCustomRequestOptions) {
  const file = opts.file.file as File
  uploadDialog.file = file
  uploadDialog.password = ''
  uploadDialog.components = allComponents.filter(c => c !== 'events')
  uploadDialog.stats = {}
  uploadDialog.needsPassword = file.name.toLowerCase().endsWith('.enc')
  uploadDialog.step = 1
  uploadDialog.show = true
  opts.onFinish()
}

async function uploadNext() {
  if (uploadDialog.step === 1) {
    // preview
    if (!uploadDialog.file) return true
    try {
      const r = await backupApi.preview(uploadDialog.file, uploadDialog.password)
      uploadDialog.stats = r.data?.stats || {}
      uploadDialog.step = 2
    } catch (e) {
      window.message?.error?.(t('backup.upload_preview_failed'))
    }
    return false
  }
  if (uploadDialog.step === 2) {
    uploadDialog.step = 3
    return false
  }
  // step 3 — execute
  if (uploadDialog.file) {
    await backupApi.upload(uploadDialog.file, uploadDialog.password, uploadDialog.components)
    window.message?.success?.(t('backup.restore_done'))
    await refresh()
  }
  uploadDialog.show = false
  return true
}

function uploadPrev() {
  if (uploadDialog.step > 1) {
    uploadDialog.step--
    return false
  }
  return true
}

// ------- Recover dialog -------

const recoverDialog = reactive({
  show: false,
  loading: false,
  id: '',
  oldPassphrase: '',
})

function openRecover(r: Backup) {
  recoverDialog.id = r.id
  recoverDialog.oldPassphrase = ''
  recoverDialog.loading = false
  recoverDialog.show = true
}

async function doRecover() {
  if (!recoverDialog.oldPassphrase || recoverDialog.oldPassphrase.length < 16) {
    // Mirror the backend's backupKeyMinLen so the user gets immediate feedback.
    window.message?.error?.(t('backup.key_missing_body'))
    return false
  }
  recoverDialog.loading = true
  try {
    const r = await backupApi.recover(recoverDialog.id, recoverDialog.oldPassphrase)
    // Replace the row in-place so the UI reflects the new keyStatus.
    if (r.data) {
      const idx = backups.value.findIndex(b => b.id === recoverDialog.id)
      if (idx >= 0) backups.value[idx] = r.data
    }
    window.message?.success?.(t('backup.recover_done'))
    await refreshKeyStatus()
    recoverDialog.show = false
    return true
  } catch (e: any) {
    // The handler already returns the localized-ish messages — just surface.
    const msg = e?.response?.data?.message || e?.message || String(e)
    window.message?.error?.(msg)
    return false
  } finally {
    recoverDialog.loading = false
  }
}

onMounted(refresh)
</script>

<style scoped>
.page-body {
  padding-top: 8px;
}
</style>
