<template>
  <!-- NOTE: the legacy asset `src/assets/login.jpg` is deprecated as of the
       "Swirl Evo" redesign and is no longer referenced from this component.
       The file is kept on disk for now and will be removed in a future release. -->
  <div class="login-wrapper">
    <n-card class="login-card" :bordered="false">
      <div class="brand">
        <img src="/favicon-source.svg" alt="Swirl Evo" class="logo" />
        <h1 class="title">Swirl Evo</h1>
        <p class="tagline">{{ t('texts.login_tagline') }}</p>
      </div>
      <n-form
        :model="model"
        ref="form"
        :rules="rules"
        label-placement="left"
        @keydown.enter="submit"
      >
        <n-form-item path="name">
          <n-input round v-model:value="model.name" :placeholder="t('fields.login_name')" clearable>
            <template #prefix>
              <n-icon>
                <person-outline />
              </n-icon>
            </template>
          </n-input>
        </n-form-item>
        <n-form-item path="password">
          <n-input
            round
            v-model:value="model.password"
            type="password"
            :placeholder="t('fields.password')"
            clearable
          >
            <template #prefix>
              <n-icon>
                <lock-closed-outline />
              </n-icon>
            </template>
          </n-input>
        </n-form-item>
        <n-button
          round
          block
          type="primary"
          :disabled="submiting"
          :loading="submiting"
          @click.prevent="submit"
        >{{ t('buttons.sign_in') }}</n-button>
        <template v-if="providers.keycloak">
          <n-divider style="margin: 18px 0"><span class="divider-text">{{ t('texts.or') }}</span></n-divider>
          <n-button round block secondary @click.prevent="loginWithKeycloak">
            {{ t('buttons.login_keycloak') }}
          </n-button>
        </template>
      </n-form>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from "vue";
import { useRouter, useRoute } from "vue-router";
import { NForm, NFormItem, NInput, NButton, NIcon, NDivider, NCard } from "naive-ui";
import { PersonOutline, LockClosedOutline } from "@vicons/ionicons5";
import userApi from "@/api/user";
import type { AuthUser } from "@/api/user";
import systemApi from "@/api/system";
import type { LoginArgs } from "@/api/user";
import { useStore } from "vuex";
import { Mutations } from "@/store/mutations";
import { useForm, requiredRule } from "@/utils/form";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter();
const route = useRoute();
const store = useStore();
const form = ref();
const model = reactive({} as LoginArgs);
const rules = {
  name: requiredRule(),
  password: requiredRule(),
};
// Routes that must never be used as post-login redirect targets: they either
// loop back to auth (login/oauth-complete) or represent dead ends (404).
// If any of these slip into ?redirect=..., default to home instead.
function safeRedirect(raw: string | null | undefined): string {
  const decoded = decodeURIComponent(raw || "/")
  if (!decoded || decoded === "/login" || decoded === "/404" || decoded === "/oauth-complete") {
    return "/"
  }
  return decoded
}

const { submit, submiting } = useForm<AuthUser>(form, () => userApi.login(model), (user: AuthUser) => {
  store.commit(Mutations.SetUser, user);
  const redirect = safeRedirect(<string>route.query.redirect);
  router.push({ path: redirect });
})

const providers = ref({ ldap: false, keycloak: false })

async function checkState() {
  const r = await systemApi.checkState();
  if (r.data?.fresh) {
    router.push("/init")
  }
}

async function loadProviders() {
  try {
    const r = await systemApi.authProviders()
    providers.value = r.data as any
  } catch { /* if endpoint unavailable, keep form-only */ }
}

function loginWithKeycloak() {
  const redirect = safeRedirect(<string>route.query.redirect)
  window.location.href = '/api/auth/keycloak/login?redirect=' + encodeURIComponent(redirect)
}

// Defensive: if the user is already authenticated (e.g. Keycloak SSO session
// silently refreshed the session token on the previous roundtrip, or the user
// hit /login while still holding a valid token) skip the form entirely and go
// straight to the target. Without this, the user sees the login page even
// though no credentials are required — which looks like "Keycloak failed"
// when the real state is "already logged in". This also plays safe when
// browsers retain back/forward cache and restore the login page after a
// successful OAuth complete.
function redirectIfAuthenticated() {
  if (store.state.user?.token) {
    const redirect = safeRedirect(<string>route.query.redirect)
    router.replace({ path: redirect })
    return true
  }
  return false
}

onMounted(() => {
  if (redirectIfAuthenticated()) return
  checkState()
  loadProviders()
});
</script>

<style lang="scss" scoped>
.login-wrapper {
  min-height: 100vh;
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  box-sizing: border-box;
  // Business-neutral gradient: uses Naive UI theme vars so it adapts to
  // both light and dark modes without hardcoded colors.
  background:
    radial-gradient(
      circle at 15% 20%,
      var(--n-color-primary) -40%,
      transparent 45%
    ),
    radial-gradient(
      circle at 85% 85%,
      var(--n-color-primary) -60%,
      transparent 50%
    ),
    linear-gradient(
      160deg,
      var(--n-body-color) 0%,
      var(--n-card-color) 100%
    );
}

.login-card {
  width: 100%;
  max-width: 440px;
  border-radius: 12px;
  box-shadow:
    0 10px 30px -12px rgba(0, 0, 0, 0.25),
    0 4px 10px -4px rgba(0, 0, 0, 0.1);
  border: 1px solid var(--n-border-color);
  // Naive UI <n-card> already uses var(--n-card-color) for background.
  :deep(.n-card__content) {
    padding: 36px 32px;
  }
}

.brand {
  display: flex;
  flex-direction: column;
  align-items: center;
  margin-bottom: 28px;

  .logo {
    width: 64px;
    height: 64px;
    display: block;
    // SVG uses #000 strokes — make it follow theme text color in dark mode
    // by applying a color-aware filter only when explicitly dark.
    filter: drop-shadow(0 2px 6px rgba(0, 0, 0, 0.15));
  }

  .title {
    margin: 14px 0 4px;
    font-size: 26px;
    font-weight: 600;
    letter-spacing: 0.3px;
    color: var(--n-text-color);
    text-align: center;
  }

  .tagline {
    margin: 0;
    font-size: 13px;
    color: var(--n-text-color-3, var(--n-text-color));
    opacity: 0.75;
    text-align: center;
  }
}

.divider-text {
  font-size: 12px;
  opacity: 0.7;
}

@media (max-width: 640px) {
  .login-wrapper {
    padding: 12px;
    align-items: flex-start;
    padding-top: 48px;
  }
  .login-card {
    max-width: 100%;
    border-radius: 10px;
    :deep(.n-card__content) {
      padding: 28px 20px;
    }
  }
  .brand {
    margin-bottom: 22px;
    .logo {
      width: 56px;
      height: 56px;
    }
    .title {
      font-size: 22px;
    }
  }
}
</style>
