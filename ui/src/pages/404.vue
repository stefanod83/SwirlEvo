<template>
  <n-result size="huge" status="404" :title="t('titles.404')" :description="t('texts.404')">
    <template #footer>
      <n-button type="primary" @click="$router.push('/')">{{ t('buttons.home') }}</n-button>
    </template>
  </n-result>
</template>

<script setup lang="ts">
import { onMounted } from "vue";
import { NResult, NButton } from "naive-ui";
import { useI18n } from 'vue-i18n'
import { useRouter } from "vue-router";
import { useStore } from "vuex";

const { t } = useI18n()
const router = useRouter()
const store = useStore()

// If the user lands on /404 while holding a valid session token, bounce
// them straight home. This covers the restart case: Swirl briefly goes down,
// the SPA falls through to /404, and when the backend comes back the user
// would otherwise get trapped here (log in → query.redirect=/404 → back to
// error page). Home is always the safe target once a session is alive.
onMounted(() => {
  if (store.state.user?.token) {
    router.replace('/')
  }
})
</script>
