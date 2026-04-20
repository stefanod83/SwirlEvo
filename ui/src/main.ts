import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router/router'
import { store } from './store'
import i18n from './locales'
import systemApi from './api/system'
import * as hostApi from './api/host'
import { Mutations } from './store/mutations'

const app = createApp(App).use(router).use(store).use(i18n);

async function bootstrap() {
  try {
    const r = await systemApi.mode()
    store.commit(Mutations.SetMode, r.data?.mode || 'swarm')
  } catch { /* keep default */ }
}

async function loadHosts() {
  if (store.state.mode !== 'standalone') return
  try {
    const r = await hostApi.search('', '', 1, 1000)
    const data = r.data as any
    // NOTE: keep in sync with store/index.ts::reloadHosts — both must
    // map the same fields. `color` drives the header bar + dropdown
    // strip + list tag; omitting it here left bootstrap loads with no
    // colour until the user re-saved a host.
    const items = (data?.items || []).map((h: any) => ({
      id: h.id,
      name: h.name,
      status: h.status,
      color: h.color || '',
    }))
    store.commit(Mutations.SetHosts, items)
  } catch { /* ignore */ }
}

// React to login (token appearing) — load hosts only when authenticated.
store.subscribe((mutation, state) => {
  if (mutation.type === Mutations.SetUser && state.user?.token) {
    loadHosts()
  } else if (mutation.type === Mutations.Logout) {
    store.commit(Mutations.SetHosts, [])
  }
})

// Mount immediately so the first paint is not blocked by the /system/mode
// bootstrap call. The store starts with mode='swarm' as a safe default; once
// bootstrap resolves it is switched to the real mode and the menu reacts.
app.mount('#app')

bootstrap().then(() => {
  // Already authenticated on refresh? Load hosts in the background.
  if (store.state.user?.token) return loadHosts()
})
