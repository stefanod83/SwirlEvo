<template>
  <div class="oauth-complete">
    <h3>{{ message }}</h3>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useRouter } from "vue-router";
import { useStore } from "vuex";
import { Mutations } from "@/store/mutations";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const store = useStore()
const message = ref(t('texts.signing_in') || 'Signing in…')

onMounted(() => {
  // Parse hash fragment: #token=...&name=...&perms=p1,p2&redirect=/x&idToken=...
  const hash = window.location.hash.startsWith('#') ? window.location.hash.slice(1) : window.location.hash
  const params = new URLSearchParams(hash)
  const token = params.get('token') || ''
  const name = params.get('name') || ''
  const permsStr = params.get('perms') || ''
  const redirect = params.get('redirect') || '/'
  const idToken = params.get('idToken') || ''

  if (!token) {
    message.value = t('texts.action_failed') || 'Login failed'
    setTimeout(() => router.push({ name: 'login' }), 1200)
    return
  }

  const perms = permsStr ? permsStr.split(',').filter(Boolean) : []
  store.commit(Mutations.SetUser, { name, token, perms })
  if (idToken) {
    try { localStorage.setItem('kc_id_token', idToken) } catch { /* noop */ }
  }
  // Clean hash before navigation so the token isn't left in browser history.
  history.replaceState(null, '', window.location.pathname)
  router.replace({ path: redirect })
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
