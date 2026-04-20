import { computed, ref, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SelfDeployDeployResult } from '@/api/self-deploy'
import { store } from '@/store'
import { Mutations } from '@/store/mutations'

// useAutoDeployProgress encapsulates the "live progress" iframe modal
// that opens when an Auto-Deploy is triggered. Two pages consume it
// (Setting.vue — when the operator deploys from Settings, and
// compose_stack/Edit.vue — when the operator clicks Auto-Deploy on
// the flagged stack) so the logic is centralised here:
//
//  - buildProgressUrl() composes the iframe URL from the backend
//    recoveryUrl shape (":8002" or "http(s)://…").
//  - /api/system/mode polling closes the modal as soon as the new
//    Swirl answers (3 s cadence). Independent of the iframe load
//    state — necessary because allow-list blocks can prevent the
//    iframe from rendering.
//  - postMessage listener matches what cmd/deploy_agent/ui/script.js
//    posts on success, so the modal can close before the first poll.
//  - Load guard + timeout guard surface a text fallback after 10 s
//    without a `load` event, and a "taking longer than expected"
//    header tag after 5 min without a success signal.
//
// The caller imports { progressOpen, openProgressFromDeployResult,
// onIframeLoad, onIframeError, cleanup, … } and binds them to the
// modal template. Cleanup MUST be called on component unmount —
// onUnmounted inside the composable handles this automatically so
// long as it's set up during component setup().
export function useAutoDeployProgress() {
    const { t } = useI18n()

    const progressOpen = ref(false)
    const progressUrl = ref('')
    const progressIframe = ref<HTMLIFrameElement | null>(null)
    const progressIframeFailed = ref(false)
    const progressIframeLoaded = ref(false)
    const progressTimedOut = ref(false)

    let progressPostMsgHandler: ((ev: MessageEvent) => void) | null = null
    let progressPollTimer: number | null = null
    let progressTimeoutTimer: number | null = null
    let progressLoadGuardTimer: number | null = null
    let lastRecoveryPort = 0

    const iframeFallbackMessage = computed(() =>
        t('self_deploy.progress.failed_to_connect', { url: progressUrl.value || '' })
    )

    function buildProgressUrl(raw: string, portHint: number): string {
        const origin = window.location
        if (!raw) {
            if (!portHint) return ''
            return `${origin.protocol}//${origin.hostname}:${portHint}/`
        }
        if (/^https?:\/\//i.test(raw)) return raw
        if (raw.startsWith(':')) {
            return `${origin.protocol}//${origin.hostname}${raw}/`
        }
        return `${origin.protocol}//${origin.hostname}:${raw}/`
    }

    function openProgressFromDeployResult(result: SelfDeployDeployResult | null | undefined) {
        const rawUrl = result?.recoveryUrl || ''
        if (rawUrl) {
            const m = rawUrl.match(/:(\d+)$/)
            if (m) lastRecoveryPort = parseInt(m[1], 10)
        }
        progressUrl.value = buildProgressUrl(rawUrl, lastRecoveryPort)
        progressIframeFailed.value = false
        progressIframeLoaded.value = false
        progressTimedOut.value = false
        progressOpen.value = true

        // Signal: a self-deploy is in flight. Axios interceptor
        // suppresses redirect-to-login on the 401 storm that hits while
        // the old Swirl container is being swapped out.
        store.commit(Mutations.SetSelfDeployInProgress, {
            jobId: result?.jobId || null,
            inProgress: true,
        })

        startProgressPolling()
        addProgressPostMessageListener()
        startProgressLoadGuard()
        startProgressTimeoutGuard()
    }

    // resumeFromSession is called by any page that mounts while the
    // store flag indicates a deploy is still in flight (typically the
    // Settings page after an accidental full-reload). It reopens the
    // modal pointing at the *last known* recovery port — best-effort
    // so the operator keeps seeing progress without re-triggering.
    function resumeFromSession() {
        if (!store.state.selfDeployInProgress) return
        const port = lastRecoveryPort || 8002
        progressUrl.value = buildProgressUrl('', port)
        progressIframeFailed.value = false
        progressIframeLoaded.value = false
        progressTimedOut.value = false
        progressOpen.value = true
        startProgressPolling()
        addProgressPostMessageListener()
        startProgressLoadGuard()
        startProgressTimeoutGuard()
    }

    function startProgressPolling() {
        stopProgressPolling()
        const tick = async () => {
            if (!progressOpen.value) return
            try {
                const resp = await fetch('/api/system/mode', { cache: 'no-store' })
                if (resp.ok) {
                    onDeploySuccess()
                    return
                }
            } catch {
                /* still down — the new container is not yet serving */
            }
        }
        tick()
        progressPollTimer = window.setInterval(tick, 3000)
    }

    function stopProgressPolling() {
        if (progressPollTimer !== null) {
            clearInterval(progressPollTimer)
            progressPollTimer = null
        }
    }

    function addProgressPostMessageListener() {
        removeProgressPostMessageListener()
        progressPostMsgHandler = (ev: MessageEvent) => {
            const d = ev.data
            if (d && typeof d === 'object' && d.type === 'swirl.self-deploy' && d.phase === 'success') {
                onDeploySuccess()
            }
        }
        window.addEventListener('message', progressPostMsgHandler)
    }

    function removeProgressPostMessageListener() {
        if (progressPostMsgHandler) {
            window.removeEventListener('message', progressPostMsgHandler)
            progressPostMsgHandler = null
        }
    }

    function startProgressLoadGuard() {
        if (progressLoadGuardTimer !== null) {
            clearTimeout(progressLoadGuardTimer)
        }
        progressLoadGuardTimer = window.setTimeout(() => {
            if (!progressIframeLoaded.value) {
                progressIframeFailed.value = true
            }
        }, 10_000)
    }

    function startProgressTimeoutGuard() {
        if (progressTimeoutTimer !== null) {
            clearTimeout(progressTimeoutTimer)
        }
        progressTimeoutTimer = window.setTimeout(() => {
            progressTimedOut.value = true
        }, 5 * 60 * 1000)
    }

    function onIframeLoad() {
        progressIframeLoaded.value = true
        progressIframeFailed.value = false
    }

    function onIframeError() {
        progressIframeFailed.value = true
    }

    function onDeploySuccess() {
        stopProgressPolling()
        removeProgressPostMessageListener()
        if (progressTimeoutTimer !== null) {
            clearTimeout(progressTimeoutTimer)
            progressTimeoutTimer = null
        }
        if (progressLoadGuardTimer !== null) {
            clearTimeout(progressLoadGuardTimer)
            progressLoadGuardTimer = null
        }
        progressOpen.value = false
        // Release the self-deploy guard BEFORE the reload so subsequent
        // requests on the fresh page follow the normal 401-redirect
        // logic again.
        store.commit(Mutations.SetSelfDeployInProgress, { jobId: null, inProgress: false })
        // Full reload so Vuex and the live setting snapshot are fresh.
        window.location.assign('/')
    }

    function cleanup() {
        stopProgressPolling()
        removeProgressPostMessageListener()
        if (progressTimeoutTimer !== null) {
            clearTimeout(progressTimeoutTimer)
            progressTimeoutTimer = null
        }
        if (progressLoadGuardTimer !== null) {
            clearTimeout(progressLoadGuardTimer)
            progressLoadGuardTimer = null
        }
    }

    onUnmounted(cleanup)

    return {
        progressOpen,
        progressUrl,
        progressIframe,
        progressIframeFailed,
        progressTimedOut,
        iframeFallbackMessage,
        openProgressFromDeployResult,
        resumeFromSession,
        onIframeLoad,
        onIframeError,
        cleanup,
    }
}
