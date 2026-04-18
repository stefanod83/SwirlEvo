<template>
  <div class="oauth-complete">
    <h3>{{ message }}</h3>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, nextTick } from "vue";
import { useRouter } from "vue-router";
import { useStore } from "vuex";
import { Mutations } from "@/store/mutations";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const store = useStore()
const message = ref(t('texts.signing_in') || 'Signing in…')

onMounted(async () => {
  // Parse hash fragment: #token=...&name=...&perms=p1,p2&redirect=/x&idToken=...
  const hash = window.location.hash.startsWith('#') ? window.location.hash.slice(1) : window.location.hash
  const params = new URLSearchParams(hash)
  const token = params.get('token') || ''
  const name = params.get('name') || ''
  const permsStr = params.get('perms') || ''
  // Guard against empty string landing here (some proxies forward ?redirect=
  // as an empty value) — fall back to home.
  const redirectParam = params.get('redirect') || ''
  const redirect = redirectParam && redirectParam !== '/oauth-complete' ? redirectParam : '/'
  const idToken = params.get('idToken') || ''

  if (!token) {
    // Fragment was stripped (some proxies drop it from 302 redirects) or
    // upstream callback failed. Fall through to the login page so the user
    // can retry — do NOT clear any existing session silently.
    message.value = t('texts.action_failed') || 'Login failed'
    setTimeout(() => router.push({ name: 'login' }), 1200)
    return
  }

  const perms = permsStr ? permsStr.split(',').filter(Boolean) : []
  // Commit first so the axios interceptor and router guard see the token
  // on the very next tick.
  store.commit(Mutations.SetUser, { name, token, perms })
  if (idToken) {
    try { localStorage.setItem('kc_id_token', idToken) } catch { /* noop */ }
  }
  // Clean hash before navigation so the token isn't left in browser history.
  history.replaceState(null, '', window.location.pathname)
  // Wait one Vue tick so the reactive state is flushed before we navigate.
  // Previously this used a fixed 100ms setTimeout — brittle on slow devices
  // and excessive on fast ones. nextTick is deterministic and enough because
  // the Vuex commit is synchronous and the axios interceptor reads the store
  // on each request (no subscription needed).
  await nextTick()
  // Use router.replace so /oauth-complete is never retained in history.
  // On success we prefer name-based navigation to '/' to avoid any path
  // matching quirks; for a custom redirect we use the path as-is.
  if (redirect === '/' || redirect === '') {
    router.replace({ name: 'home' })
  } else {
    router.replace({ path: redirect })
  }
})
</script>

<style scoped>
.oauth-complete {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100vh;
  font-size: 14px;
  color: #666;
}
</style>
