import { computed, ref, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SelfDeployDeployResult } from '@/api/self-deploy'
import { store } from '@/store'
import { Mutations } from '@/store/mutations'

// useAutoDeployProgress owns the "deploy in progress" modal shown while
// the self-deploy sidekick swaps out the primary Swirl container.
// v3-simplified: no iframe, no allow-list, no sidekick HTTP server.
//
// Polling (both via plain `fetch` to bypass the axios interceptor, with
// the session token set explicitly since fetch does not see the axios
// request interceptor):
//
//  - `/api/self-deploy/status` every 2 s — reads state.json via the
//    primary Swirl. Populates the phase chip + recent log lines.
//    During the brief window when the primary is being swapped, the
//    fetch fails transparently — the modal keeps the last-seen phase.
//  - `/api/system/mode` every 3 s — the "is the new primary up?"
//    signal. First 200 OK triggers onDeploySuccess → close modal +
//    full reload.
//
// An internal `tick` ref drives the elapsed-time computed so the
// displayed counter updates once a second.
export function useAutoDeployProgress() {
    const { t } = useI18n()

    const progressOpen = ref(false)
    const progressTimedOut = ref(false)
    const progressStatus = ref('')
    const progressPhase = ref('')
    const progressError = ref('')
    const progressLogTail = ref<string[]>([])
    const currentJobId = ref('')
    const progressStartedAt = ref(0)
    const nowTick = ref(0)

    let progressPollTimer: number | null = null
    let statusPollTimer: number | null = null
    let elapsedTimer: number | null = null
    let progressTimeoutTimer: number | null = null

    // sawInProgress flips true the first time the status poll observes
    // an in-flight phase. Until that happens we must IGNORE any 200
    // from /api/system/mode — the old primary is still answering, and
    // closing the modal now would reload the page in the middle of
    // the container swap ("Bad Gateway" from the reverse proxy).
    const IN_PROGRESS_PHASES = new Set([
        'pending', 'stopping', 'pulling', 'starting', 'health_check', 'recovery',
    ])
    const TERMINAL_FAIL_PHASES = new Set(['failed', 'rolled_back'])
    let sawInProgress = false

    const progressDescription = computed(() =>
        t('self_deploy.progress.description')
    )

    const progressElapsed = computed(() => {
        // Depending on nowTick (bumped every second) forces a
        // recomputation so the UI counter ticks forward in real time.
        void nowTick.value
        if (!progressStartedAt.value) return ''
        const secs = Math.max(0, Math.round((Date.now() - progressStartedAt.value) / 1000))
        const m = Math.floor(secs / 60)
        const s = secs % 60
        return `${m}:${String(s).padStart(2, '0')}`
    })

    const progressPhaseLabel = computed(() => {
        if (!progressPhase.value) return ''
        const key = `self_deploy.status.${progressPhase.value}`
        const v = t(key)
        return v === key ? progressPhase.value : v
    })

    function openProgressFromDeployResult(result: SelfDeployDeployResult | null | undefined) {
        currentJobId.value = result?.jobId || ''
        progressTimedOut.value = false
        progressStatus.value = t('self_deploy.progress.waiting_initial')
        progressPhase.value = 'pending'
        progressError.value = ''
        progressLogTail.value = []
        progressStartedAt.value = Date.now()
        sawInProgress = false
        progressOpen.value = true

        store.commit(Mutations.SetSelfDeployInProgress, {
            jobId: result?.jobId || null,
            inProgress: true,
        })

        startProgressPolling()
        startStatusPolling()
        startElapsedTicker()
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
        progressPhase.value = ''
        progressError.value = ''
        progressLogTail.value = []
        progressStartedAt.value = Date.now()
        // When resuming after a full page reload we assume the deploy
        // already moved past the in-progress handshake — the session
        // flag could only have been persisted if the deploy was in
        // flight. A subsequent /status poll will confirm.
        sawInProgress = true
        progressOpen.value = true

        startProgressPolling()
        startStatusPolling()
        startElapsedTicker()
        startProgressTimeoutGuard()
    }

    // authHeaders reads the session token directly from Vuex because
    // fetch() bypasses the axios request interceptor.
    function authHeaders(): HeadersInit {
        const token = store.state.user?.token
        return token ? { Authorization: `Bearer ${token}` } : {}
    }

    function startProgressPolling() {
        stopProgressPolling()
        const tick = async () => {
            if (!progressOpen.value) return
            try {
                const resp = await fetch('/api/system/mode', { cache: 'no-store' })
                if (resp.ok) {
                    // CRITICAL: do NOT close the modal on the very
                    // first 200 OK — the old primary is still running
                    // for a brief window between /deploy returning 202
                    // and the sidekick calling docker stop on it. If
                    // we reload now, the browser hits the reverse
                    // proxy mid-stop → Bad Gateway. Gate the success
                    // on sawInProgress (flipped by the /status poll
                    // when it observes pending/stopping/etc).
                    if (sawInProgress) {
                        onDeploySuccess()
                        return
                    }
                    progressStatus.value = t('self_deploy.progress.waiting_initial')
                } else {
                    progressStatus.value = t('self_deploy.progress.waiting_503')
                }
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

    function startStatusPolling() {
        stopStatusPolling()
        const tick = async () => {
            if (!progressOpen.value) return
            try {
                const resp = await fetch('/api/self-deploy/status', {
                    cache: 'no-store',
                    headers: authHeaders(),
                })
                if (!resp.ok) return
                const body = await resp.json()
                const data = body?.data
                if (!data) return
                if (typeof data.phase === 'string' && data.phase) {
                    progressPhase.value = data.phase
                    // Flip the sawInProgress flag so a later 200 from
                    // /api/system/mode (or a `success` phase read
                    // from this poll) is treated as the true "deploy
                    // completed" signal — not a spurious hit on the
                    // old primary before the sidekick stopped it.
                    if (IN_PROGRESS_PHASES.has(data.phase)) {
                        sawInProgress = true
                    }
                    if (data.phase === 'success' && sawInProgress) {
                        onDeploySuccess()
                        return
                    }
                    if (TERMINAL_FAIL_PHASES.has(data.phase) && sawInProgress) {
                        // Terminal failure — stop polling but keep
                        // the modal open with the error/logs visible
                        // so the operator can read them. The store
                        // flag is cleared so the router guard releases
                        // and the user can navigate away manually.
                        stopProgressPolling()
                        stopStatusPolling()
                        stopElapsedTicker()
                        store.commit(Mutations.SetSelfDeployInProgress, { jobId: null, inProgress: false })
                        return
                    }
                }
                if (typeof data.error === 'string') {
                    progressError.value = data.error
                }
                if (Array.isArray(data.logTail)) {
                    progressLogTail.value = data.logTail.slice(-10)
                }
                if (data.jobId && !currentJobId.value) {
                    currentJobId.value = data.jobId
                }
            } catch {
                /* primary down or transient error — modal keeps last-seen values */
            }
        }
        tick()
        statusPollTimer = window.setInterval(tick, 2000)
    }

    function stopStatusPolling() {
        if (statusPollTimer !== null) {
            clearInterval(statusPollTimer)
            statusPollTimer = null
        }
    }

    function startElapsedTicker() {
        stopElapsedTicker()
        elapsedTimer = window.setInterval(() => { nowTick.value++ }, 1000)
    }

    function stopElapsedTicker() {
        if (elapsedTimer !== null) {
            clearInterval(elapsedTimer)
            elapsedTimer = null
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
        stopStatusPolling()
        stopElapsedTicker()
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
        stopStatusPolling()
        stopElapsedTicker()
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
        progressPhase,
        progressPhaseLabel,
        progressError,
        progressLogTail,
        currentJobId,
        openProgressFromDeployResult,
        resumeFromSession,
        cleanup,
    }
}
