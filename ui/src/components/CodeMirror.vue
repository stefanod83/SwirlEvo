<template>
  <div ref="wrapperRef" :style="wrapperStyle">
    <textarea ref="editorRef" />
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onBeforeUnmount, onMounted, ref, toRefs, watch } from "vue";
import { useThemeVars } from "naive-ui";
import { useStore } from "vuex";
// CodeMirror: common
import CodeMirror from "codemirror";
import "codemirror/mode/yaml/yaml.js";
import "codemirror/lib/codemirror.css";
import "codemirror/theme/seti.css";
// CodeMirror: fold
import "codemirror/addon/fold/foldgutter.css";
import "codemirror/addon/fold/foldcode.js";
import "codemirror/addon/fold/brace-fold.js";
import "codemirror/addon/fold/comment-fold.js";
import "codemirror/addon/fold/indent-fold.js";
import "codemirror/addon/fold/foldgutter.js";
// CodeMirror: search
import "codemirror/addon/scroll/annotatescrollbar.js";
import "codemirror/addon/search/matchesonscrollbar.js";
import "codemirror/addon/search/match-highlighter.js";
import "codemirror/addon/search/jump-to-line.js";
import "codemirror/addon/dialog/dialog.js";
import "codemirror/addon/dialog/dialog.css";
import "codemirror/addon/search/searchcursor.js";
import "codemirror/addon/search/search.js";

export default defineComponent({
  props: {
    modelValue: String,
    defaultValue: {
      type: String,
      default: '',
    },
    readonly: {
      type: Boolean,
      default: false
    },
    height: {
      type: String,
      default: '',
    }
  },
  setup(props, context) {
    const themeVars = useThemeVars()
    const store = useStore();
    const { modelValue, defaultValue, readonly, height } = toRefs(props);
    const editorRef = ref();
    const wrapperRef = ref<HTMLElement>();
    let editor: CodeMirror.EditorFromTextArea | null;
    let resizeObserver: ResizeObserver | null = null;

    // Wrapper style: owns the concrete height so the inner
    // CodeMirror (which the global `.CodeMirror` rule stretches to
    // `height: 100%`) fills it exactly. `box-sizing: border-box` +
    // `overflow: hidden` make the 1 px border count inside the
    // declared height — otherwise the editor would either overflow
    // or leave a 2 px gap at the bottom. Callers may override
    // `height` by passing `:style="{ height: '55vh' }"` on the
    // component — Vue's style merge lets parent styles win over
    // these defaults.
    const wrapperStyle = computed(() => ({
      border: `1px solid ${themeVars.value.borderColor}`,
      width: '100%',
      height: height.value || '450px',
      boxSizing: 'border-box' as const,
      overflow: 'hidden' as const,
    }));

    // Track the preference store so the CodeMirror theme updates on the fly
    // when the user toggles dark/light mode without a page refresh. Without
    // this, the editor stays on whichever theme was active at mount time —
    // resulting in a white-on-white (or dark-on-dark) editor after toggling.
    const cmTheme = computed(() =>
      store.state.preference.theme === 'dark' ? 'seti' : 'default'
    );

    watch(modelValue, () => {
      if (null != editor && modelValue.value && modelValue.value !== editor.getValue()) {
        editor.setValue(modelValue.value);
      }
    });
    watch(readonly, () => {
      if (null != editor) {
        editor.setOption("readOnly", readonly.value);
      }
    });
    watch(cmTheme, (v) => {
      if (null != editor) {
        editor.setOption("theme", v);
      }
    });
    onMounted(() => {
      editor = CodeMirror.fromTextArea(editorRef.value, {
        value: modelValue.value,
        indentWithTabs: false,
        smartIndent: true,
        lineNumbers: true,
        readOnly: readonly.value,
        foldGutter: true,
        lineWrapping: true,
        gutters: ["CodeMirror-linenumbers", "CodeMirror-foldgutter", "CodeMirror-lint-markers"],
        theme: cmTheme.value,
      });
      editor.on("change", () => {
        context.emit("update:modelValue", editor?.getValue());
      });
      // If modelValue was populated before the editor was mounted (typical when
      // CodeMirror lives inside a lazy-rendered tab), the watch above will not
      // trigger — push the value here.
      if (modelValue.value) {
        editor.setValue(modelValue.value);
      } else if (defaultValue.value) {
        editor.setValue(defaultValue.value);
      }
      // Height is owned by the wrapper style + `.CodeMirror { height:
      // 100% }` rule in common.css — NOT by editor.setSize. The old
      // setSize call was redundant and fought the `!important` rule
      // on odd viewport sizes (editor fixed to setSize value while
      // wrapper was a vh-based height → gap or overflow).
      // Force a refresh so the editor lays out correctly when mounted inside
      // a previously-hidden tab pane.
      setTimeout(() => editor?.refresh(), 50);
      // Re-layout when the outer wrapper changes width (window resize,
      // sidebar toggle, form-item reflow). Without this, CodeMirror
      // keeps the width it saw at mount time and drifts out of
      // alignment with the form's borders — the gutters end up
      // misaligned or the editor overflows the form content box.
      if (typeof ResizeObserver !== 'undefined' && wrapperRef.value) {
        resizeObserver = new ResizeObserver(() => {
          editor?.refresh();
        });
        resizeObserver.observe(wrapperRef.value);
      }
    });
    onBeforeUnmount(() => {
      if (resizeObserver !== null) {
        resizeObserver.disconnect();
        resizeObserver = null;
      }
      if (null !== editor) {
        editor.toTextArea();
        editor = null;
      }
    });
    return { wrapperStyle, editorRef, wrapperRef };
  }
});
</script>

