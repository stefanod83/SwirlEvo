<template>
  <n-config-provider
    :theme="theme"
    :locale="locale"
    :date-locale="dateLocale"
    :theme-overrides="themeFixed"
    :hljs="hljs"
  >
    <n-loading-bar-provider>
      <n-message-provider>
        <n-notification-provider>
          <n-dialog-provider>
            <Root />
          </n-dialog-provider>
        </n-notification-provider>
      </n-message-provider>
    </n-loading-bar-provider>
    <n-global-style />
  </n-config-provider>
</template>

<script lang="ts">
import { computed, defineComponent, h } from "vue";
import {
  zhCN,
  dateZhCN,
  itIT,
  dateItIT,
  NConfigProvider,
  NDialogProvider,
  NNotificationProvider,
  NMessageProvider,
  NLoadingBarProvider,
  NGlobalStyle,
  useMessage,
  useLoadingBar,
  useDialog,
  useNotification,
} from "naive-ui";
import { darkTheme } from "naive-ui";
import { useRoute } from "vue-router";
import { useStore } from "vuex";
import hljs from 'highlight.js/lib/core'
import json from 'highlight.js/lib/languages/json'
import yaml from 'highlight.js/lib/languages/yaml'
import bash from 'highlight.js/lib/languages/bash'
import { initLoadingBar } from "@/router/router";
import DefaultLayout from "./layouts/Default.vue";
import SimpleLayout from "./layouts/Simple.vue";
import EmptyLayout from "./layouts/Empty.vue";

const layouts: Record<string, any> = {
  default: DefaultLayout,
  simple: SimpleLayout,
  empty: EmptyLayout,
}

const Root = defineComponent({
  name: "Root",
  setup() {
    window.message = useMessage();
    window.dialog = useDialog();
    window.notification = useNotification();
    initLoadingBar(useLoadingBar());

    const route = useRoute();
    const layout = computed(() => layouts[(route.meta.layout as string) || "default"] || DefaultLayout);
    return () => h(layout.value);
  },
})

export default defineComponent({
  name: "App",
  components: {
    NConfigProvider,
    NDialogProvider,
    NNotificationProvider,
    NMessageProvider,
    NLoadingBarProvider,
    NGlobalStyle,
    Root,
  },
  setup() {
    const store = useStore();
    const theme = computed(() => store.state.preference?.theme === "dark" ? darkTheme : null);
    const locale = computed(() => {
      const l = store.state.preference?.locale
      if (l === 'zh') return zhCN
      if (l === 'it') return itIT
      return null
    });
    const dateLocale = computed(() => {
      const l = store.state.preference?.locale
      if (l === 'zh') return dateZhCN
      if (l === 'it') return dateItIT
      return null
    });
    const themeFixed = {
      Form: {
        feedbackHeightMedium: "20px",
        feedbackFontSizeMedium: "12px",
        // blankHeightMedium: "30px",
      },
    }

    hljs.registerLanguage('json', json)
    hljs.registerLanguage('yaml', yaml)
    hljs.registerLanguage('bash', bash)

    return {
      locale,
      dateLocale,
      theme,
      themeFixed,
      hljs,
    };
  },
});
</script>

<style>
@import '@/assets/common.css';
#app {
  font-family: Avenir, Helvetica, Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}
</style>