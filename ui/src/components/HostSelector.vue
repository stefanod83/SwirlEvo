<template>
  <n-select
    v-if="visible"
    size="small"
    :consistent-menu-width="false"
    :options="options"
    :value="currentValue"
    :placeholder="t('objects.host')"
    :render-label="renderLabel"
    @update:value="onChange"
    style="width: 220px"
  />
</template>

<script setup lang="ts">
import { h, computed } from "vue";
import { NSelect } from "naive-ui";
import { useStore } from "vuex";
import { useI18n } from 'vue-i18n'
import { Mutations } from "@/store/mutations";

const { t } = useI18n()
const store = useStore()

const visible = computed(() => store.state.mode === 'standalone' && store.state.hosts.length >= 1)
const currentValue = computed(() => store.state.selectedHostId as any)

const options = computed(() => {
  const hosts = store.state.hosts as any[]
  const opts: any[] = []
  if (hosts.length >= 2) {
    opts.push({ label: t('fields.all_hosts'), value: null, status: '' })
  }
  for (const h of hosts) {
    opts.push({ label: h.name, value: h.id, status: h.status })
  }
  return opts
})

function renderLabel(opt: any) {
  if (opt.value === null) {
    return h('span', null, opt.label)
  }
  const color = opt.status === 'connected' ? '#18a058' : (opt.status === 'error' ? '#d03050' : '#999')
  return h('span', { style: 'display:flex;align-items:center;gap:6px' }, [
    h('span', { style: `width:8px;height:8px;border-radius:50%;background:${color};display:inline-block` }),
    opt.label,
  ])
}

function onChange(v: string | null) {
  store.commit(Mutations.SetSelectedHost, v)
}
</script>
