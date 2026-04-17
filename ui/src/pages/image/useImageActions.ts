import { reactive, ref } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import imageApi from '@/api/image'
import type { Image } from '@/api/image'
import registryApi from '@/api/registry'

export interface TagDialogState {
  show: boolean
  loading: boolean
  source: string
  target: string
}

export interface PushDialogState {
  show: boolean
  loading: boolean
  ref: string
  refOptions: { label: string; value: string }[]
  registryId: string
}

export interface RegistryOption {
  label: string
  value: string
  url: string
}

/**
 * Shared logic for the image Tag/Push modals. Used by both
 * `pages/image/List.vue` and `pages/image/View.vue`.
 *
 * The consumer supplies the Docker host/node id via `nodeProvider` (a getter
 * — so the value is read lazily each time an action fires, rather than
 * captured at composable-instantiation time when the host may not be known
 * yet). An optional `onDone` callback fires after a successful tag (to
 * refetch list data); push does not need it because the image list doesn't
 * change after a push.
 */
export function useImageActions(
  nodeProvider: () => string,
  onDone?: () => void,
) {
  const { t } = useI18n()
  const message = useMessage()

  // ------- Tag dialog -------
  const tagDialog = reactive<TagDialogState>({
    show: false,
    loading: false,
    source: '',
    target: '',
  })

  function openTagDialog(i: Image) {
    tagDialog.source = (i.tags && i.tags[0]) || i.id
    tagDialog.target = ''
    tagDialog.loading = false
    tagDialog.show = true
  }

  async function doTag(): Promise<boolean> {
    if (!tagDialog.target.trim()) {
      message.error(t('image.tag_target_required'))
      return false
    }
    tagDialog.loading = true
    try {
      await imageApi.tag(nodeProvider(), tagDialog.source, tagDialog.target.trim())
      message.success(t('texts.action_success'))
      tagDialog.show = false
      onDone?.()
      return true
    } catch (e: any) {
      message.error(e?.message || String(e))
      return false
    } finally {
      tagDialog.loading = false
    }
  }

  // ------- Push dialog -------
  const registryOptions = ref<RegistryOption[]>([])
  const pushDialog = reactive<PushDialogState>({
    show: false,
    loading: false,
    ref: '',
    refOptions: [],
    registryId: '',
  })

  async function ensureRegistries() {
    if (registryOptions.value.length) return
    try {
      const r = await registryApi.search()
      registryOptions.value = (r.data || []).map((x: any) => ({
        label: `${x.name} (${x.url})`,
        value: x.id,
        url: x.url,
      }))
    } catch {
      /* leave empty; user can still push anonymously */
    }
  }

  async function openPushDialog(i: Image) {
    await ensureRegistries()
    pushDialog.refOptions = (i.tags || []).map(tg => ({ label: tg, value: tg }))
    pushDialog.ref = pushDialog.refOptions[0]?.value || ''
    pushDialog.registryId = ''
    pushDialog.loading = false
    pushDialog.show = true
  }

  async function doPush(): Promise<boolean> {
    if (!pushDialog.ref.trim()) {
      message.error(t('image.push_ref_required'))
      return false
    }
    pushDialog.loading = true
    try {
      await imageApi.push(nodeProvider(), pushDialog.ref.trim(), pushDialog.registryId)
      message.success(t('image.push_done'))
      pushDialog.show = false
      return true
    } catch (e: any) {
      message.error(e?.message || String(e))
      return false
    } finally {
      pushDialog.loading = false
    }
  }

  return {
    tagDialog,
    pushDialog,
    registryOptions,
    openTagDialog,
    openPushDialog,
    doTag,
    doPush,
    ensureRegistries,
  }
}
