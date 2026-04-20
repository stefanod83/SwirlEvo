import { computed, ref, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SelfDeployDeployResult } from '@/api/self-deploy'
import { store } from '@/store'
import { Mutations } from '@/store/mutations'

// useAutoDeployProgress owns the minimal "deploy in progress" modal
// shown while the self-deploy sidekick swaps out the primary Swirl
// container. v3-simplified: no iframe, no allow-list, no sidekick HTTP
// server. Just:
//
//  - `openProgressFromDeployResult` sets the global in-progress flag
//    (so the axios interceptor silences transient errors and the
//    router guard pins the user to Settings) and starts polling
//    `/api/system/mode` every 3 seconds via `fetch` (bypasses axios).
//  - The modal shows an indeterminate spinner + status text + a
//    timeout-warning tag after 5 minutes. No content is pulled from
//    the sidekick.
//  - On the first 200 response from `/api/system/mode` the modal
//    closes, the flag is cleared, and the page full-reloads on `/`
//    so Vuex + settings are refreshed against the new primary.
//  - `resumeFromSession` re-opens the modal when the Settings page
//    mounts during an active deploy (e.g. after a full reload).
export function useAutoDeployProgress() {
    const { t } = useI18n()

    const progressOpen = ref(false)
    const progressTimedOut = ref(false)
    const progressStatus = ref('')
    const currentJobId = ref('')

    let progressPollTimer: number | null = null
    let progressTimeoutTimer: number | null = null
    let progressStartedAt = 0

    const progressDescription = computed(() =>
        t('self_deploy.progress.description')
    )

    const progressElapsed = computed(() => {
        if (!progressStartedAt) return ''
        const secs = Math.max(0, Math.round((Date.now() - progressStartedAt) / 1000))
        const m = Math.floor(secs / 60)
        const s = secs % 60
        return `${m}:${String(s).padStart(2, '0')}`
    })

    function openProgressFromDeployResult(result: SelfDeployDeployResult | null | undefined) {
        currentJobId.value = result?.jobId || ''
        progressTimedOut.value = false
        progressStatus.value = t('self_deploy.progress.waiting_initial')
        progressStartedAt = Date.now()
        progressOpen.value = true

        store.commit(Mutations.SetSelfDeployInProgress, {
            jobId: result?.jobId || null,
            inProgress: true,
        })

        startProgressPolling()
        startProgressTimeoutGuard()
    }

    // resumeFromSession is called by Settings on mount when the store
    // flag indicates a deploy is still in flight (typically after a
    // full-reload mid-deploy).
    function resumeFromSession() {
        if (!store.state.selfDeployInProgress) return
        currentJobId.value = store.state.selfDeployJobId || ''
        progressTimedOut.value = false
        progressStatus.value = t('self_deploy.progress.waiting_restored')
        progressStartedAt = Date.now()
        progressOpen.value = true

        startProgressPolling()
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
                progressStatus.value = t('self_deploy.progress.waiting_503')
            } catch {
                progressStatus.value = t('self_deploy.progress.waiting_connecting')
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

    function startProgressTimeoutGuard() {
        if (progressTimeoutTimer !== null) {
            clearTimeout(progressTimeoutTimer)
        }
        progressTimeoutTimer = window.setTimeout(() => {
            progressTimedOut.value = true
        }, 5 * 60 * 1000)
    }

    function onDeploySuccess() {
        stopProgressPolling()
        if (progressTimeoutTimer !== null) {
            clearTimeout(progressTimeoutTimer)
            progressTimeoutTimer = null
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
        if (progressTimeoutTimer !== null) {
            clearTimeout(progressTimeoutTimer)
            progressTimeoutTimer = null
        }
    }

    onUnmounted(cleanup)

    return {
        progressOpen,
        progressStatus,
        progressDescription,
        progressElapsed,
        progressTimedOut,
        currentJobId,
        openProgressFromDeployResult,
        resumeFromSession,
        cleanup,
    }
}
