import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router/router'
import { store } from './store'
import i18n from './locales'
import systemApi from './api/system'
import { Mutations } from './store/mutations'

const app = createApp(App).use(router).use(store).use(i18n);

systemApi.mode()
  .then(r => store.commit(Mutations.SetMode, r.data?.mode || 'swarm'))
  .catch(() => { /* keep default */ })
  .finally(() => app.mount('#app'));
