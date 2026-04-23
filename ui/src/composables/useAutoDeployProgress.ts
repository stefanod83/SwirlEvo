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
//  - `/api/system/ready` every 3 s — the "is the new primary
//    fully-initialised?" signal. Gated on the sidekick having written
//    an in-progress phase (`sawInProgress`) so the old primary
//    answering a 200 pre-stop doesn't cause a too-early reload. First
//    200 OK AFTER `sawInProgress` triggers onDeploySuccess → close
//    modal + full reload. /ready (not /mode) is used because /mode
//    answers as soon as the HTTP server starts — before the DB
//    client and settings snapshot are wired up — which caused the
//    "home page loads broken, need F5" race.
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
    // a phase where the OLD primary is guaranteed down. Until then, any
    // 200 from /api/system/ready is coming from the old primary (still
    // serving during `pending` / `stopping`) and MUST be ignored —
    // otherwise the UI redirects mid-stop and the reverse proxy throws
    // Bad Gateway for the operator.
    //
    // Phases `pending` and `stopping` are DELIBERATELY excluded:
    //   - pending  = sidekick just launched, has not touched anything
    //   - stopping = docker stop is in flight (30s graceful window);
    //                the old process can still answer /ready until it
    //                actually exits.
    // First safe signal is `pulling` — set by the sidekick AFTER
    // stopPrimary + renamePrimary have both returned successfully.
    // `success` is included so fast deploys that skip through the
    // intermediate phases between two /status polls still arm the
    // fast-path correctly.
    const POST_STOP_PHASES = new Set([
        'pulling', 'starting', 'health_check', 'recovery', 'success',
    ])
    const TERMINAL_FAIL_PHASES = new Set(['failed', 'rolled_back'])
    let sawInProgress = false
    // Require N CUMULATIVE 200s from /api/system/ready via the reverse
    // proxy before declaring the deploy done. Cumulative (not
    // consecutive) because reverse proxies (Traefik etc.) can briefly
    // flap between old and new container during the swap — a single
    // 404/502 in the middle shouldn't nuke the whole settling window
    // or the UI gets stuck waiting forever. At 3 s polling × 3 samples
    // ≈ 9 s minimum of observed readiness before redirect, tolerating
    // up to a few transient failures along the way.
    const READY_CONFIRMS_REQUIRED = 3
    let readyConfirms = 0
    // Fast-path: once the sidekick has declared phase=success (both
    // its gates passed — HTTP probe + Docker healthcheck), a single
    // 200 from /ready via the reverse proxy is enough to confirm
    // routing landed on the new container. Also arms a
    // safety-valve timeout: if Traefik is genuinely stuck on the
    // old routing table, redirect anyway after the grace period so
    // the operator isn't left watching an infinite spinner when the
    // backend is actually fine.
    const FAST_PATH_CONFIRMS = 1
    const SIDEKICK_SUCCESS_GRACE_MS = 15_000
    let sidekickSuccessAt: number | null = null

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
        readyConfirms = 0
        sidekickSuccessAt = null
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
        readyConfirms = 0
        sidekickSuccessAt = null
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
                const resp = await fetch('/api/system/ready', { cache: 'no-store' })
                if (resp.ok) {
                    // CRITICAL: do NOT close the modal on the very
                    // first 200 OK — two reasons.
                    // (1) The old primary is still running for a brief
                    // window between /deploy returning 202 and the
                    // sidekick calling docker stop on it; reloading
                    // now would hit the reverse proxy mid-stop → Bad
                    // Gateway. Gate on `sawInProgress` (flipped by
                    // the /status poll when it observes
                    // pending/stopping/etc).
                    // (2) The new primary answers /ready as soon as
                    // the DB is pinged and the Docker client is
                    // constructed (~2 s after container start). That
                    // is too early in practice: biz-layer caches are
                    // cold, and the reverse proxy can still be
                    // flapping between the old and new container.
                    // Require N cumulative 200s before redirecting so
                    // readiness is sustained through those windows.
                    if (sawInProgress) {
                        readyConfirms++
                        // Fast path: once the sidekick has declared
                        // success (both gates cleared), one 200 via
                        // the reverse proxy is enough to confirm
                        // routing has landed on the new container.
                        const threshold = sidekickSuccessAt !== null
                            ? FAST_PATH_CONFIRMS
                            : READY_CONFIRMS_REQUIRED
                        if (readyConfirms >= threshold) {
                            onDeploySuccess()
                            return
                        }
                        progressStatus.value = t('self_deploy.progress.waiting_settling')
                    } else {
                        progressStatus.value = t('self_deploy.progress.waiting_initial')
                    }
                } else {
                    // Non-200: do NOT reset readyConfirms — a reverse
                    // proxy flap mid-swap shouldn't discard a partial
                    // settling window and leave the UI stuck forever.
                    // Safety valve: if the sidekick has already
                    // reported success and the grace window has
                    // elapsed without /ready cooperating (Traefik
                    // genuinely stuck on stale routes), redirect
                    // anyway — the backend is fine.
                    if (sidekickSuccessAt !== null && Date.now() - sidekickSuccessAt > SIDEKICK_SUCCESS_GRACE_MS) {
                        onDeploySuccess()
                        return
                    }
                    progressStatus.value = t('self_deploy.progress.waiting_503')
                }
            } catch {
                // Same logic for transport-level errors (connection
                // refused mid-swap, DNS blip): don't reset, apply
                // the same safety valve.
                if (sidekickSuccessAt !== null && Date.now() - sidekickSuccessAt > SIDEKICK_SUCCESS_GRACE_MS) {
                    onDeploySuccess()
                    return
                }
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
                    // Flip sawInProgress only once the OLD primary is
                    // guaranteed down (see POST_STOP_PHASES). Before
                    // this point, /ready 200s come from the old
                    // primary still serving and MUST be ignored so we
                    // do not redirect mid-swap.
                    if (POST_STOP_PHASES.has(data.phase)) {
                        sawInProgress = true
                    }
                    if (data.phase === 'success' && sawInProgress) {
                        // Do NOT redirect from here — the sidekick's
                        // own probe is against the container's direct
                        // IP, which returns 200 before the reverse
                        // proxy (Traefik) has picked up the new
                        // container. Let the /ready poll (which goes
                        // through the reverse proxy) drive the
                        // redirect.
                        //
                        // We DO arm the fast-path + safety-valve
                        // timer here: once the sidekick has declared
                        // success (both gates cleared), the /ready
                        // poll only needs one more 200 to redirect,
                        // and if Traefik never cooperates within the
                        // grace window, the safety valve redirects
                        // anyway so the operator isn't stuck.
                        if (sidekickSuccessAt === null) {
                            sidekickSuccessAt = Date.now()
                        }
                        return
                    }
                    if (TERMINAL_FAIL_PHASES.has(data.phase)) {
                        // Terminal failure — stop polling but keep
                        // the modal open with the error/logs visible
                        // so the operator can read them. The store
                        // flag is cleared so the router guard releases
                        // and the user can navigate away manually.
                        // NOT gated on sawInProgress: a deploy that
                        // fails during `stopping` (or fails before
                        // reaching any POST_STOP_PHASE) would
                        // otherwise leave the modal stuck without
                        // surfacing the error.
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
        // Full reload with a cache-busting query param so the browser
        // doesn't serve the old index.html (which references stale
        // content-hashed chunks from the previous build — those 404
        // against the new container's embedded ui/dist). Query param
        // forces a conditional GET on index.html; the new bundle's
        // fresh chunks are then loaded normally.
        window.location.assign('/?_r=' + Date.now())
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
