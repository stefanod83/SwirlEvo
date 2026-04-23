import { computed, ref, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SelfDeployDeployResult } from '@/api/self-deploy'
import { store } from '@/store'
import { Mutations } from '@/store/mutations'

// useAutoDeployProgress owns the "deploy in progress" modal shown while
// the self-deploy sidekick swaps out the primary Swirl container.
//
// Redirect-readiness model (v4). The old check only looked at
// `/api/system/ready` — that endpoint returns 200 as soon as the HTTP
// server is up + DB reachable, which happens BEFORE the static-asset
// middleware is registered. The first redirect then landed on a bare
// 404 because `/` hadn't yet been wired to serve `index.html`. The
// browser showed the 404 page and the UX read like the deploy had
// failed.
//
// The new model requires THREE gates, all probed together every
// 3 s, and accepted only when all three succeed CONSECUTIVELY for N
// rounds (a single failure resets the streak — we cannot redirect on a
// flapping proxy or a half-initialised container):
//
//  Gate A — sidekick phase post-stop (`sawInProgress`) so the old
//           primary still answering on 200 cannot be confused with
//           the new one. Driven by `/api/self-deploy/status` poll.
//  Gate B — backend ready: `/api/system/ready` → 200 over the reverse
//           proxy. Confirms DB + Docker client + settings are wired.
//  Gate C — UI bundle alive: fetch `/`, parse out the first module
//           bundle (`/assets/index-<hash>.js`), HEAD-like fetch it.
//           This is the gate the old flow missed — the new container
//           is only fully "ready to serve the SPA" once the static
//           middleware exposes the hashed bundle.
//
// Fast-path & safety valve:
//
//  - Once the sidekick reports `phase=success` (both its own gates
//    cleared — HTTP probe + docker healthcheck), reduce the required
//    consecutive rounds from N to 1. Traefik has likely already
//    picked up the new container, a single all-green round is
//    enough.
//  - If 30 s elapse after `phase=success` without Gates B+C going
//    green together, stop trying to auto-redirect and surface a
//    manual "Reload now" button so the operator can decide. The
//    previous safety valve redirected anyway and that's what was
//    causing the 404: a sustained Gate C failure meant the SPA
//    wasn't actually up yet.
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
    // Require N CONSECUTIVE all-green rounds (Gates B+C both OK) to
    // redirect. Consecutive (not cumulative): a single Gate failure
    // resets the streak. The old cumulative logic would count a 200
    // from a half-initialised container as a confirm, reach the
    // threshold, and redirect → 404 on /assets/* because the static
    // middleware wasn't wired yet.
    const READY_CONFIRMS_REQUIRED = 3
    // Fast-path: once the sidekick has declared phase=success (both
    // its own gates cleared — HTTP probe + docker healthcheck), one
    // consecutive all-green round is enough.
    const FAST_PATH_CONFIRMS = 1
    // After `phase=success`, if the browser-side gates (B+C) never go
    // green together within this grace window, stop auto-redirect
    // attempts and surface a "Reload now" hint so the operator can
    // decide. No forced redirect — the old safety valve forced a
    // reload on a server whose UI wasn't actually serving the bundle,
    // which is exactly the 404 trap we were falling into.
    const SIDEKICK_SUCCESS_GRACE_MS = 30_000
    let readyConfirms = 0
    let sidekickSuccessAt: number | null = null
    const readyStuck = ref(false)

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
        readyStuck.value = false
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
        readyStuck.value = false
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
            // Gate A already tracked by sawInProgress (set by /status
            // poll). When false, skip everything — /ready from the old
            // primary pre-stop would otherwise confirm prematurely.
            if (!sawInProgress) {
                progressStatus.value = t('self_deploy.progress.waiting_initial')
                return
            }

            // Gate B: backend ready endpoint.
            let gateB = false
            try {
                const resp = await fetch('/api/system/ready', { cache: 'no-store' })
                gateB = resp.ok
            } catch {
                gateB = false
            }

            // Gate C: SPA is actually served (index.html + main bundle).
            // Only runs when Gate B passed, so on a brief proxy flap
            // we skip the asset fetch entirely (gentler on the old
            // container still answering /).
            const gateC = gateB ? await probeSpaBundle() : false

            if (gateB && gateC) {
                readyConfirms++
                const threshold = sidekickSuccessAt !== null
                    ? FAST_PATH_CONFIRMS
                    : READY_CONFIRMS_REQUIRED
                if (readyConfirms >= threshold) {
                    onDeploySuccess()
                    return
                }
                progressStatus.value = t('self_deploy.progress.waiting_settling')
            } else {
                // Any gate miss resets the streak — consecutive
                // all-green is the whole point: a half-initialised
                // container answering /ready 200 without serving the
                // bundle was the 404 trap.
                readyConfirms = 0

                // After phase=success, if the grace window has
                // elapsed and gates still aren't green, stop trying
                // to auto-redirect and let the operator decide.
                // Consumers watch `readyStuck` and surface a manual
                // "Reload now" control.
                if (
                    sidekickSuccessAt !== null
                    && Date.now() - sidekickSuccessAt > SIDEKICK_SUCCESS_GRACE_MS
                    && !readyStuck.value
                ) {
                    readyStuck.value = true
                    progressStatus.value = t('self_deploy.progress.stuck_manual_reload')
                    return
                }
                progressStatus.value = gateB
                    ? t('self_deploy.progress.waiting_assets')
                    : t('self_deploy.progress.waiting_503')
            }
        }
        tick()
        progressPollTimer = window.setInterval(tick, 3000)
    }

    // probeSpaBundle confirms the new primary is serving the SPA
    // end-to-end: `/` returns an HTML document, we extract the main
    // module bundle referenced by <script type="module" src="…">, and
    // verify the asset responds 200. This is the gate the old flow
    // was missing — `/api/system/ready` goes green several seconds
    // before the static-asset middleware is registered, so the prior
    // redirect consistently landed on 404.
    async function probeSpaBundle(): Promise<boolean> {
        try {
            const r = await fetch('/?_probe=' + Date.now(), {
                cache: 'no-store',
                headers: { Accept: 'text/html' },
                redirect: 'manual',
            })
            if (!r.ok) return false
            const ct = r.headers.get('Content-Type') || ''
            if (!ct.includes('text/html')) return false
            const html = await r.text()
            // Vite emits `<script type="module" crossorigin src="/assets/index-<hash>.js">`.
            const m = /<script[^>]+src="(\/assets\/[^"]+\.js)"/i.exec(html)
            if (!m) return false
            const assetResp = await fetch(m[1], { cache: 'no-store' })
            // Cancel the download: we only care about the status code.
            try { await assetResp.body?.cancel() } catch { /* noop */ }
            return assetResp.ok
        } catch {
            return false
        }
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

    // reloadNow is invoked by the modal's "Reload now" button when the
    // operator wants to bypass the automatic gate check. Same teardown
    // as onDeploySuccess — we only split them so the UI can offer an
    // explicit escape hatch during the stuck-after-success state.
    function reloadNow() {
        onDeploySuccess()
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
        readyStuck,
        openProgressFromDeployResult,
        resumeFromSession,
        reloadNow,
        cleanup,
    }
}
