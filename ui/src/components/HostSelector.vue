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
    opts.push({ label: t('fields.all_hosts'), value: null, status: '', color: '' })
  }
  for (const h of hosts) {
    opts.push({ label: h.name, value: h.id, status: h.status, color: h.color || '' })
  }
  return opts
})

// renderLabel draws: [host-color tag] [connection-status dot] <name>
//   - The host-color tag is a 3x12 vertical bar coloured with the
//     operator-chosen host colour. Absent when the host has no
//     colour set (or for the "All hosts" entry).
//   - The connection-status dot is the green/red/grey circle that
//     already existed — indicates reachability of the daemon.
// Together the two signals give the operator independent reads on
// "which host is this" (color tag) and "is it online" (status dot).
function renderLabel(opt: any) {
  if (opt.value === null) {
    return h('span', null, opt.label)
  }
  const statusColor = opt.status === 'connected' ? '#18a058' : (opt.status === 'error' ? '#d03050' : '#999')
  const children: any[] = []
  if (opt.color) {
    children.push(h('span', {
      style: `width:3px;height:14px;border-radius:2px;background:${opt.color};display:inline-block`,
    }))
  }
  children.push(h('span', {
    style: `width:8px;height:8px;border-radius:50%;background:${statusColor};display:inline-block`,
  }))
  children.push(opt.label)
  return h('span', { style: 'display:flex;align-items:center;gap:6px' }, children)
}

function onChange(v: string | null) {
  store.commit(Mutations.SetSelectedHost, v)
}
</script>
