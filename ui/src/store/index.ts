import { createStore, createLogger } from 'vuex'
import { Mutations } from "./mutations";
import * as hostApi from '@/api/host'

const debug = import.meta.env.DEV

interface User {
    name: string;
    token: string;
    perms: Set<string>;
}

export interface HostOption {
    id: string;
    name: string;
    status: string;
}

export interface State {
    user?: User | null;
    preference: {
        theme: string | null;
        locale: string | null;
    }
    ajaxLoading: boolean;
    mode: string;
    selectedHostId: string | null;   // null = "All"
    hosts: HostOption[];
    // selfDeployInProgress: true while an auto-deploy is in flight. The
    // ajax 401 interceptor uses this to skip the normal redirect-to-
    // login behaviour — Swirl's own container is being swapped out and
    // transient 401/connection-refused are expected. Persisted in
    // sessionStorage with a 10-minute TTL so a page reload during the
    // swap restores the progress modal instead of booting the operator
    // back to home.
    selfDeployInProgress: boolean;
    selfDeployJobId: string | null;
}

function loadObject(key: string) {
    let value = null
    try {
        value = JSON.parse(localStorage.getItem(key) as string)
    } catch {
    }
    return value
}

function initState(): State {
    const user = Object.assign({}, loadObject('user'))
    const locale = navigator.language.startsWith('zh') ? 'zh' : 'en'
    const savedHost = localStorage.getItem('selectedHost')
    // Read self-deploy progress from sessionStorage. TTL is 10 minutes —
    // long enough to cover a typical deploy (<~3 min), short enough that
    // a crashed tab does not lock future sessions into the "in progress"
    // branch of the interceptor forever.
    let sdInProgress = false
    let sdJobId: string | null = null
    try {
        const raw = sessionStorage.getItem('swirl.selfDeploy')
        if (raw) {
            const rec = JSON.parse(raw) as { jobId: string; expiresAt: number }
            if (rec && rec.expiresAt && rec.expiresAt > Date.now()) {
                sdInProgress = true
                sdJobId = rec.jobId || null
            } else {
                sessionStorage.removeItem('swirl.selfDeploy')
            }
        }
    } catch {
        sessionStorage.removeItem('swirl.selfDeploy')
    }
    return {
        user: { perms: new Set(user.perms), name: user.name, token: user.token },
        preference: Object.assign({ theme: 'light', locale: locale }, loadObject('preference')),
        ajaxLoading: false,
        mode: 'swarm',
        selectedHostId: savedHost && savedHost !== 'null' ? savedHost : null,
        hosts: [],
        selfDeployInProgress: sdInProgress,
        selfDeployJobId: sdJobId,
    }
}

export const store = createStore<State>({
    strict: debug,
    state: initState(),
    getters: {
        anonymous(state) {
            return !state.user?.token
        },
        allow(state) {
            return (perm: string) => state.user?.perms?.has('*') || state.user?.perms?.has(perm)
        },
    },
    mutations: {
        [Mutations.Logout](state) {
            localStorage.removeItem("user");
            state.user = null;
        },
        [Mutations.SetUser](state, user) {
            localStorage.setItem("user", JSON.stringify(user));
            state.user = { perms: new Set(user.perms), name: user.name, token: user.token };
        },
        [Mutations.SetPreference](state, preference) {
            localStorage.setItem("preference", JSON.stringify(preference));
            state.preference = preference;
        },
        [Mutations.SetAjaxLoading](state, loading) {
            state.ajaxLoading = loading;
        },
        [Mutations.SetMode](state, mode) {
            state.mode = mode;
        },
        [Mutations.SetSelectedHost](state, hostId: string | null) {
            state.selectedHostId = hostId;
            if (hostId === null) {
                localStorage.removeItem('selectedHost');
            } else {
                localStorage.setItem('selectedHost', hostId);
            }
        },
        [Mutations.SetSelfDeployInProgress](state, payload: { jobId: string | null; inProgress: boolean }) {
            state.selfDeployInProgress = !!payload?.inProgress
            state.selfDeployJobId = payload?.jobId || null
            if (payload?.inProgress) {
                try {
                    sessionStorage.setItem('swirl.selfDeploy', JSON.stringify({
                        jobId: payload.jobId || '',
                        expiresAt: Date.now() + 10 * 60 * 1000,
                    }))
                } catch { /* quota-exceeded: best-effort only */ }
            } else {
                sessionStorage.removeItem('swirl.selfDeploy')
            }
        },
        [Mutations.SetHosts](state, hosts: HostOption[]) {
            state.hosts = hosts || [];
            // Reconcile saved selection: if the saved host no longer exists, fall back.
            if (state.selectedHostId && !state.hosts.find(h => h.id === state.selectedHostId)) {
                state.selectedHostId = null;
                localStorage.removeItem('selectedHost');
            }
            // Auto-select if only one host is configured.
            if (state.hosts.length === 1) {
                state.selectedHostId = state.hosts[0].id;
                localStorage.setItem('selectedHost', state.hosts[0].id);
            }
        },
    },
    actions: {
        async reloadHosts({ commit, state }) {
            if (state.mode !== 'standalone') return
            try {
                const r = await hostApi.search('', '', 1, 1000)
                const data = r.data as any
                const items = (data?.items || []).map((h: any) => ({ id: h.id, name: h.name, status: h.status }))
                commit(Mutations.SetHosts, items)
            } catch { /* ignore */ }
        },
    },
    plugins: debug ? [createLogger()] : [],
})
