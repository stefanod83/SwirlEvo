<template>
  <n-layout :position="isMobile ? 'static' : 'absolute'">
    <n-layout-header bordered>
      <div class="header-left" align="center">
        <n-popover
          v-if="isMobile || isTablet"
          style="padding: 0; width: 200px"
          placement="bottom-end"
          display-directive="show"
          trigger="click"
          ref="menuPopover"
        >
          <template #trigger>
            <n-button size="small" style="margin-right: 8px">
              <template #icon>
                <n-icon>
                  <menu-outline />
                </n-icon>
              </template>
            </n-button>
          </template>
          <div style="overflow: auto; max-height: 79vh">
            <n-menu
              :value="menuValue"
              :options="menuOptions"
              :indent="18"
              @update:value="menuPopover.setShow(false)"
              :render-label="renderMenuLabel"
            />
          </div>
        </n-popover>
        <n-text tag="div" class="logo" :depth="1" @click="$router.push('/')">
          <img src="/favicon.ico" v-if="!isMobile" />
          SwirlEvo
        </n-text>
        <x-host-selector style="margin-left: 16px" />
      </div>
      <n-space justify="end" align="center" class="header-right" :size="0">
        <div style="margin-right: 10px; line-height: 56px">
          <n-text depth="3">v{{ version.version }}</n-text>
        </div>
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button
              type="default"
              size="small"
              :bordered="false"
              tag="a"
              href="https://github.com/stefanod83/SwirlEvo"
              target="_blank"
            >
              <template #icon>
                <n-icon>
                  <LogoGithub />
                </n-icon>
              </template>
            </n-button>
          </template>
          GitHub
        </n-tooltip>
        <n-dropdown @select="selectOption" trigger="hover" :options="dropdownOptions" show-arrow>
          <n-button quaternary size="small">
            <template #icon>
              <n-icon>
                <PersonOutline />
              </n-icon>
            </template>
            {{ store.state.user?.name }}
          </n-button>
        </n-dropdown>
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button size="small" quaternary @click="logout">
              <template #icon>
                <n-icon>
                  <LogOutOutline />
                </n-icon>
              </template>
            </n-button>
          </template>
          {{ t('buttons.sign_out') }}
        </n-tooltip>
      </n-space>
    </n-layout-header>
    <!-- Host-colour marker bar: visible only when the global host
         selector has an active selection AND that host has a custom
         colour set. The bar sits just below the header so the
         operator's eye catches it before clicking any destructive
         action. Empty colour = no bar at all (no layout shift). -->
    <div
      v-if="activeHostColor"
      class="host-color-bar"
      :style="{ backgroundColor: activeHostColor }"
      :title="activeHostName"
    />
    <n-layout
      has-sider
      :position="isMobile ? 'static' : 'absolute'"
      :style="layoutContentStyle"
    >
      <n-layout-sider
        v-if="!isMobile && !isTablet"
        bordered
        width="200"
        :collapsed-width="64"
        :collapsed="collapsed"
        collapse-mode="width"
        show-trigger="bar"
        trigger-style="right: -25px"
        collapsed-trigger-style="right: -25px"
        @collapse="collapsed = true"
        @expand="collapsed = false"
      >
        <n-menu
          :value="menuValue"
          :options="menuOptions"
          :collapsed="collapsed"
          :collapsed-width="64"
          :collapsed-icon-size="22"
          :root-indent="20"
          :indent="24"
          :render-label="renderMenuLabel"
          :expanded-keys="expandedKeys"
          @update:expanded-keys="updateExpandedKeys"
        />
      </n-layout-sider>
      <n-layout-content>
        <router-view></router-view>
        <n-back-top :right="16" :bottom="10" />
      </n-layout-content>
    </n-layout>
    <n-layout-footer bordered :position="isMobile ? 'static' : 'absolute'">
      <span>{{ t('copyright') }}</span>
    </n-layout-footer>
  </n-layout>
</template>

<script setup lang="ts">
import { ref, computed, reactive, watch, onMounted } from "vue";
import {
  NButton,
  NIcon,
  NMenu,
  NText,
  NSpace,
  NLayout,
  NLayoutHeader,
  NLayoutSider,
  NLayoutContent,
  NLayoutFooter,
  NPopover,
  NTooltip,
  NDropdown,
  NSwitch,
  NBackTop,
} from "naive-ui";
import { MenuOutline, PersonOutline, LogOutOutline, LogoGithub } from "@vicons/ionicons5";
import { RouterView, useRouter, useRoute } from "vue-router";
import { useStore } from "vuex";
import { useIsMobile, useIsTablet } from "@/utils";
import { findMenuValue, renderMenuLabel, buildMenuOptions, findActiveOptions } from "@/router/menu";
import XHostSelector from "@/components/HostSelector.vue";
import systemApi from "@/api/system";
import type { Version } from "@/api/system";
import { Mutations } from "@/store/mutations";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const dropdownOptions = [
  {
    label: t('titles.profile'),
    key: "profile",
  },
];
const store = useStore();
const router = useRouter();
const route = useRoute();
const menuPopover = ref();
const collapsed = ref(false)
const expandedKeys = ref([] as string[]);
const isMobile = useIsMobile()
const isTablet = useIsTablet()
const darkTheme = computed(() => store.state.preference.theme === "dark")
const menuOptions = computed(() => buildMenuOptions(store.state.mode, activeHost.value?.type))
const menuValue = computed(() => findMenuValue(menuOptions.value, route))
const version = ref({} as Version);

// Active host + its colour (for the host-colour marker bar under the
// header). Returns '' when no host is selected or when the selected
// host has no custom colour — the bar is hidden accordingly, no
// layout shift.
const activeHost = computed(() => {
  const id = store.state.selectedHostId
  if (!id) return null
  return store.state.hosts.find((h: any) => h.id === id) || null
})
const activeHostColor = computed(() => activeHost.value?.color || '')
const activeHostName = computed(() => activeHost.value?.name || '')

// The content layout starts below the header (56 px) + bar height
// (4 px when present). Using a computed style lets the bar be
// inserted/removed without a manual height recalculation.
const BAR_HEIGHT = 4
const layoutContentStyle = computed(() => {
  if (isMobile.value) return ''
  const top = 56 + (activeHostColor.value ? BAR_HEIGHT : 0)
  return `top: ${top}px; bottom: 64px`
})

function updateExpandedKeys(data: any) {
  expandedKeys.value = data
}

function selectOption(key: any) {
  switch (key as string) {
    case "profile":
      router.push("/profile")
      return
    default:
      console.info(key)
  }
}

async function logout() {
  // If we have a Keycloak id_token, try to fetch the RP-initiated logout URL.
  // Failure falls back to local logout only.
  let kcLogoutURL = ''
  const idToken = (() => { try { return localStorage.getItem('kc_id_token') || '' } catch { return '' } })()
  if (idToken) {
    try {
      const r = await systemApi.authProviders()
      if (r.data?.keycloak) {
        const resp = await fetch('/api/auth/keycloak/logout-url?idToken=' + encodeURIComponent(idToken) + '&redirect=' + encodeURIComponent(window.location.origin + '/login'))
        const body = await resp.json()
        if (body?.data?.url) kcLogoutURL = body.data.url
      }
    } catch { /* ignore — fall back to local */ }
    try { localStorage.removeItem('kc_id_token') } catch { /* noop */ }
  }
  store.commit(Mutations.Logout);
  if (kcLogoutURL) {
    window.location.href = kcLogoutURL
  } else {
    router.push("/login");
  }
}

function ensureActiveExpanded() {
  const keys = findActiveOptions(menuOptions.value, route).map((opt: any) => opt.key) as string[]
  // union with user-expanded keys, so manual expansion is preserved but active
  // parent is always open.
  const union = new Set([...expandedKeys.value, ...keys])
  expandedKeys.value = Array.from(union)
}

watch(() => route.path, ensureActiveExpanded, { immediate: true })

onMounted(async () => {
  const r = await systemApi.version();
  version.value = r.data as Version;
})
</script>

<style scoped>
::v-deep(.header-right .n-button__content) {
  margin-top: 4px;
}
.header-left {
  flex-grow: 1;
  width: 180px;
  display: flex;
  align-items: center;
}
.header-right {
  width: 320px;
}
/* Host-colour marker bar: 4 px solid line under the header, same
   width as the viewport. `position: absolute` with `top: 56px`
   matches the header height so the bar renders in the narrow gap
   between header and content. `z-index` keeps it above the content
   but below any dropdowns. `transition` smooths colour changes when
   the operator switches between hosts. */
.host-color-bar {
  position: absolute;
  top: 56px;
  left: 0;
  right: 0;
  height: 4px;
  z-index: 10;
  transition: background-color 200ms ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.18);
}
/* .n-layout-header {
  background-color: #363636;
} */
.n-layout-sider {
  box-shadow: 2px 0 4px -2px rgb(10 10 10 / 10%);
}
.n-layout-footer {
  box-shadow: 0px -2px 4px -2px rgb(10 10 10 / 10%);
  /* background-image: radial-gradient(circle at 1% 1%,#328bf2,#1644ad); */
}
/* .n-layout-header {
  background-image: linear-gradient(to right, rgb(91, 121, 162) 0%, rgb(46, 68, 105) 100%);
}
.logo {
  color: white;
}
.n-layout-header .n-icon {
  color: white;
}
::v-deep(.n-layout-header .n-button__content) {
  color: white;
}
::v-deep(.n-layout-header .n-button__content:hover) {
  color: green;
} */
</style>
