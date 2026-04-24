<template>
  <!--
    PasswordInput — reusable password widget for local-user forms
    (create user / change password / initial setup). Wraps an
    <n-input type="password"> with four affordances operators keep
    asking for:
      1. Quick-picker for special characters, so keyboard layouts
         that hide `@ # $ & |` behind modifier combos aren't a
         roadblock. Characters are inserted at the cursor position.
      2. Password generator (16 chars, guarantees at least one of
         each class). Replaces the field's current value.
      3. Copy-to-clipboard. Especially useful right after Generate
         so the operator can paste the new password in their
         password manager before saving.
      4. Strength meter (0–4) rendered as a thin bar + label. Pure
         client-side heuristic — no external "have-i-been-pwned"
         round-trip.
  -->
  <div class="pw-field">
    <n-input-group>
      <n-input
        ref="inputRef"
        :value="modelValue"
        @update:value="onInput"
        @focus="showPicker = true"
        type="password"
        show-password-on="click"
        :placeholder="placeholder"
        :size="size"
        :input-props="{ autocomplete: 'new-password' }"
      />
      <n-tooltip trigger="hover">
        <template #trigger>
          <n-button
            :size="size"
            quaternary
            :focusable="false"
            @click="showPicker = !showPicker"
          >
            <template #icon>
              <n-icon><AtIcon /></n-icon>
            </template>
          </n-button>
        </template>
        {{ t('password.symbols') }}
      </n-tooltip>
      <n-tooltip trigger="hover">
        <template #trigger>
          <n-button
            :size="size"
            quaternary
            :focusable="false"
            @click="generate"
          >
            <template #icon>
              <n-icon><KeyIcon /></n-icon>
            </template>
          </n-button>
        </template>
        {{ t('password.generate') }}
      </n-tooltip>
      <n-tooltip trigger="hover">
        <template #trigger>
          <n-button
            :size="size"
            quaternary
            :focusable="false"
            :disabled="!modelValue"
            @click="copy"
          >
            <template #icon>
              <n-icon><CopyIcon /></n-icon>
            </template>
          </n-button>
        </template>
        {{ t('password.copy') }}
      </n-tooltip>
    </n-input-group>

    <!-- Special-char quick picker: click a glyph to insert it at
         the cursor. Collapsible so the field stays compact when
         not in use. -->
    <div v-if="showPicker" class="pw-picker">
      <n-button
        v-for="c of SPECIAL_CHARS"
        :key="c"
        size="tiny"
        quaternary
        :focusable="false"
        class="pw-picker-btn"
        @click="insertChar(c)"
      >
        <span class="pw-picker-ch">{{ c }}</span>
      </n-button>
    </div>

    <!-- Strength meter: only when the field has content. -->
    <div v-if="modelValue" class="pw-strength">
      <div class="pw-strength-bar">
        <div
          class="pw-strength-fill"
          :style="{ width: strengthPct + '%', background: strengthColor }"
        ></div>
      </div>
      <span class="pw-strength-label" :style="{ color: strengthColor }">
        {{ strengthLabel }}
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { NInput, NInputGroup, NButton, NTooltip, NIcon, useMessage } from 'naive-ui'
import {
  KeyOutline as KeyIcon,
  CopyOutline as CopyIcon,
  AtOutline as AtIcon,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'

const props = withDefaults(defineProps<{
  modelValue?: string
  placeholder?: string
  size?: 'tiny' | 'small' | 'medium' | 'large'
  length?: number
}>(), {
  modelValue: '',
  placeholder: '',
  size: 'medium',
  length: 16,
})
const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
}>()

const { t } = useI18n()
const message = useMessage()

const inputRef = ref<InstanceType<typeof NInput> | null>(null)
const showPicker = ref(false)

// Default special-char set. Covers everything the Unicode-in-layout
// layouts hide behind modifiers (IT / DE / FR keyboards) and keeps
// the password-safe subset Docker / compose env-file parsers accept.
const SPECIAL_CHARS = [
  '!', '@', '#', '$', '%', '&', '*', '-', '_', '+', '=',
  '(', ')', '[', ']', '{', '}', ';', ':', ',', '.',
  '/', '?', '~', '|', '^', '<', '>',
]

function onInput(v: string) {
  emit('update:modelValue', v)
}

// insertChar splices the character at the current cursor position in
// the underlying <input>. Falls back to append when the DOM element
// isn't reachable (e.g. ref not yet resolved).
function insertChar(ch: string) {
  const el = inputRef.value?.inputElRef as HTMLInputElement | undefined
  if (!el) {
    emit('update:modelValue', (props.modelValue || '') + ch)
    return
  }
  const start = el.selectionStart ?? el.value.length
  const end = el.selectionEnd ?? el.value.length
  const next = el.value.slice(0, start) + ch + el.value.slice(end)
  emit('update:modelValue', next)
  // Restore caret just after the inserted char after Vue commits.
  requestAnimationFrame(() => {
    try {
      el.focus()
      const pos = start + ch.length
      el.setSelectionRange(pos, pos)
    } catch { /* noop */ }
  })
}

// generate creates a random password of `props.length` guaranteed
// to contain at least one uppercase, one lowercase, one digit, and
// one symbol from SPECIAL_CHARS — the minimum every password policy
// in practice asks for. Uses crypto.getRandomValues so the output is
// cryptographically random (Math.random would be predictable).
function generate() {
  const lower = 'abcdefghijklmnopqrstuvwxyz'
  const upper = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'
  const digit = '0123456789'
  const sym = SPECIAL_CHARS.join('')
  const pools = [lower, upper, digit, sym]
  const all = lower + upper + digit + sym
  const n = Math.max(props.length, 8)
  const out: string[] = []
  // Pick one char from each class first so the constraint is met.
  for (const p of pools) {
    out.push(p[rand(p.length)])
  }
  for (let i = out.length; i < n; i++) {
    out.push(all[rand(all.length)])
  }
  // Fisher–Yates shuffle — keeps per-class guarantees but randomises
  // which position each class lands in.
  for (let i = out.length - 1; i > 0; i--) {
    const j = rand(i + 1)
    ;[out[i], out[j]] = [out[j], out[i]]
  }
  emit('update:modelValue', out.join(''))
}

function rand(max: number): number {
  const buf = new Uint32Array(1)
  crypto.getRandomValues(buf)
  return buf[0] % max
}

async function copy() {
  if (!props.modelValue) return
  try {
    await navigator.clipboard.writeText(props.modelValue)
    message.success(t('password.copied'))
  } catch {
    message.error(t('password.copy_failed'))
  }
}

// Strength score 0–4. The heuristic is intentionally simple: length
// floors + class diversity. Not a substitute for zxcvbn but enough
// to flag obviously weak entries without pulling a 200 KB dep.
const score = computed(() => {
  const v = props.modelValue || ''
  if (!v) return 0
  let s = 0
  if (v.length >= 8) s++
  if (v.length >= 12) s++
  if (/[A-Z]/.test(v) && /[a-z]/.test(v) && /[0-9]/.test(v)) s++
  if (/[^A-Za-z0-9]/.test(v)) s++
  return s
})
const strengthPct = computed(() => Math.max(5, (score.value / 4) * 100))
const strengthColor = computed(() => {
  switch (score.value) {
    case 0:
    case 1: return '#d03050'
    case 2: return '#f0a020'
    case 3: return '#18a058'
    default: return '#2080f0'
  }
})
const strengthLabel = computed(() => {
  switch (score.value) {
    case 0:
    case 1: return t('password.weak')
    case 2: return t('password.fair')
    case 3: return t('password.good')
    default: return t('password.strong')
  }
})
</script>

<style scoped>
.pw-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.pw-picker {
  display: flex;
  flex-wrap: wrap;
  gap: 2px;
  padding: 4px 0;
}
.pw-picker-btn {
  min-width: 26px;
  padding: 0 4px;
}
.pw-picker-ch {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
}
.pw-strength {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
}
.pw-strength-bar {
  flex: 1;
  height: 3px;
  background: rgba(128, 128, 128, 0.18);
  border-radius: 2px;
  overflow: hidden;
}
.pw-strength-fill {
  height: 100%;
  transition: width 120ms ease, background 120ms ease;
}
.pw-strength-label {
  min-width: 60px;
  text-align: right;
  font-weight: 500;
}
</style>
