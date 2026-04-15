<template>
  <n-space vertical :size="12">
    <n-alert type="info">
      {{ t('tips.topology') }}
    </n-alert>
    <n-space justify="space-between" align="center">
      <n-space :size="6" align="center">
        <n-tag :bordered="false" type="info">
          <template #icon><n-icon><server-outline /></n-icon></template>
          {{ t('objects.host', 1) || 'Host' }}
        </n-tag>
        <n-tag :bordered="false" type="success">
          <template #icon><n-icon><globe-outline /></n-icon></template>
          {{ t('objects.network', 1) || 'Network' }}
        </n-tag>
        <n-tag :bordered="false" type="warning">
          <template #icon><n-icon><cube-outline /></n-icon></template>
          {{ t('objects.container', 1) || 'Container' }}
        </n-tag>
        <n-tag :bordered="false" type="error">
          <template #icon><n-icon><alert-circle-outline /></n-icon></template>
          {{ t('fields.exposed') }}
        </n-tag>
      </n-space>
      <n-space :size="8">
        <n-button secondary size="small" @click="fetchData" :loading="loading">
          <template #icon><n-icon><refresh-outline /></n-icon></template>
          {{ t('buttons.refresh') || 'Refresh' }}
        </n-button>
      </n-space>
    </n-space>

    <div ref="chartEl" class="topology-canvas" />

    <n-card v-if="selected" size="small" :title="selectedTitle">
      <template #header-extra>
        <n-button text size="small" @click="selected = null">
          <template #icon><n-icon><close-outline /></n-icon></template>
        </n-button>
      </template>
      <n-descriptions :column="2" size="small" bordered>
        <n-descriptions-item v-for="(v, k) in selectedFields" :key="k" :label="String(k)">
          <span style="white-space: pre-wrap">{{ v }}</span>
        </n-descriptions-item>
      </n-descriptions>
      <n-space v-if="selected.flags && selected.flags.length" :size="6" style="margin-top: 10px">
        <n-tag v-for="f in selected.flags" :key="f" :type="flagTagType(f)" :bordered="false">
          {{ flagLabel(f) }}
        </n-tag>
      </n-space>
    </n-card>
  </n-space>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import * as echarts from "echarts";
import {
  NSpace, NButton, NIcon, NCard, NTag, NAlert,
  NDescriptions, NDescriptionsItem,
  useMessage,
} from "naive-ui";
import {
  RefreshOutline, CloseOutline,
  ServerOutline, GlobeOutline, CubeOutline, AlertCircleOutline,
} from "@vicons/ionicons5";
import { useResizeObserver } from "@vueuse/core";
import networkApi from "@/api/network";
import type { NetworkTopology, NetworkTopologyNode, NetworkTopologyEdge } from "@/api/network";
import { useStore } from "vuex";
import { useI18n } from "vue-i18n";

const props = defineProps<{ hostId: string | null }>();
const { t } = useI18n();
const message = useMessage();
const store = useStore();

const chartEl = ref<HTMLElement | null>(null);
let chart: echarts.ECharts | null = null;
const loading = ref(false);
const selected = ref<NetworkTopologyNode | null>(null);

// Race guard: drop stale responses when the user rapidly switches host.
let requestGen = 0;

const selectedTitle = computed(() => {
  if (!selected.value) return "";
  const type = selected.value.type;
  const typeLabel = type === "host" ? "Host" : type === "network" ? "Network" : "Container";
  return `${typeLabel} · ${selected.value.label}`;
});

const selectedFields = computed<Record<string, string>>(() => {
  if (!selected.value) return {};
  const out: Record<string, string> = {
    ID: selected.value.id.replace(/^(host|net|ct):/, ""),
  };
  const meta = selected.value.meta || {};
  for (const [k, v] of Object.entries(meta)) {
    if (v === null || v === undefined || v === "") continue;
    if (k === "ports" && Array.isArray(v)) {
      out[k] = v
        .map((p: any) => `${p.ip || "0.0.0.0"}:${p.publicPort} → ${p.privatePort}/${p.type}`)
        .join("\n");
    } else if (k === "ipam" && Array.isArray(v)) {
      out[k] = v
        .map((c: any) => `subnet=${c.subnet || "-"}, gateway=${c.gateway || "-"}`)
        .join("\n");
    } else if (typeof v === "object") {
      out[k] = JSON.stringify(v);
    } else {
      out[k] = String(v);
    }
  }
  return out;
});

function flagLabel(f: string): string {
  switch (f) {
    case "exposed-public": return t("fields.exposed");
    case "local-only": return t("fields.local_only");
    case "isolated": return t("fields.isolated");
    case "ingress": return t("fields.ingress");
    default: return f;
  }
}

function flagTagType(f: string): "error" | "warning" | "info" | "success" | "default" {
  switch (f) {
    case "exposed-public": return "error";
    case "local-only": return "success";
    case "isolated": return "info";
    case "ingress": return "warning";
    default: return "default";
  }
}

function buildOption(topo: NetworkTopology): echarts.EChartsOption {
  const data = topo.nodes.map((n: NetworkTopologyNode) => {
    const exposed = n.flags?.includes("exposed-public");
    const isolated = n.flags?.includes("isolated");
    const category = exposed ? 3 : n.type === "host" ? 0 : n.type === "network" ? 1 : 2;
    const symbolSize = n.type === "host" ? 56 : n.type === "network" ? 36 : 22;
    let symbol: string;
    if (n.type === "host") symbol = "roundRect";
    else if (n.type === "network") symbol = "diamond";
    else symbol = "circle";
    return {
      id: n.id,
      name: n.label,
      category,
      symbol,
      symbolSize,
      itemStyle: isolated ? { borderColor: "#3b82f6", borderWidth: 2 } : undefined,
      value: { meta: n.meta, type: n.type, flags: n.flags, label: n.label },
    };
  });

  const links = topo.edges.map((e: NetworkTopologyEdge) => ({
    source: e.source,
    target: e.target,
    lineStyle: {
      type: e.type === "host-network" ? "dashed" : "solid",
      width: e.type === "network-container" ? 1.2 : 1,
      opacity: 0.7,
    },
    label: e.label ? { show: true, formatter: e.label, fontSize: 10 } : undefined,
  }));

  return {
    tooltip: {
      trigger: "item",
      formatter: (p: any) => {
        if (!p.data?.value) return p.name || "";
        const v = p.data.value;
        const t = v.type as string;
        const parts: string[] = [`<b>${v.label}</b> <small>(${t})</small>`];
        if (v.flags?.length) {
          parts.push(
            v.flags
              .map((f: string) => `<span style="color:#ef4444">${flagLabel(f)}</span>`)
              .join(" · "),
          );
        }
        const meta = v.meta || {};
        if (t === "network") {
          if (meta.driver) parts.push(`driver: ${meta.driver}`);
          if (meta.scope) parts.push(`scope: ${meta.scope}`);
          if (meta.ipam?.length) {
            const first = meta.ipam[0];
            if (first.subnet) parts.push(`subnet: ${first.subnet}`);
          }
        } else if (t === "container") {
          if (meta.image) parts.push(`image: ${meta.image}`);
          if (meta.state) parts.push(`state: ${meta.state}`);
          if (meta.ports?.length) {
            const p0 = meta.ports
              .map((p: any) => `${p.ip || "0.0.0.0"}:${p.publicPort}→${p.privatePort}/${p.type}`)
              .join("<br/>");
            parts.push(`ports:<br/>${p0}`);
          }
        } else if (t === "host") {
          if (meta.containerCount !== undefined) parts.push(`containers: ${meta.containerCount}`);
          if (meta.networkCount !== undefined) parts.push(`networks: ${meta.networkCount}`);
        }
        return parts.join("<br/>");
      },
    },
    legend: { show: false },
    animation: true,
    series: [
      {
        type: "graph",
        layout: "force",
        roam: true,
        draggable: true,
        focusNodeAdjacency: true,
        categories: [
          { name: "host" },
          { name: "network" },
          { name: "container" },
          { name: "exposed" },
        ],
        force: {
          repulsion: 240,
          gravity: 0.06,
          edgeLength: [80, 180],
        },
        label: {
          show: true,
          position: "right",
          fontSize: 11,
        },
        emphasis: {
          focus: "adjacency",
          lineStyle: { width: 2 },
        },
        data,
        links,
      },
    ],
    color: ["#3b82f6", "#10b981", "#f97316", "#ef4444"],
  } as echarts.EChartsOption;
}

async function fetchData() {
  if (!props.hostId) {
    chart?.clear();
    return;
  }
  const myGen = ++requestGen;
  loading.value = true;
  try {
    const r = await networkApi.topology(props.hostId);
    if (myGen !== requestGen) return;
    const topo = r.data as NetworkTopology;
    if (!chart) return;
    chart.setOption(buildOption(topo), true);
  } catch (e: any) {
    if (myGen !== requestGen) return;
    message.error(e?.message || String(e));
  } finally {
    if (myGen === requestGen) loading.value = false;
  }
}

function initChart() {
  if (!chartEl.value) return;
  const theme = store.state.preference?.theme || "light";
  chart = echarts.init(chartEl.value, theme);
  chart.on("click", (params: any) => {
    if (!params.data?.value) return;
    const n = params.data;
    selected.value = {
      id: n.id,
      type: n.value.type,
      label: n.value.label,
      meta: n.value.meta,
      flags: n.value.flags,
    };
  });
}

useResizeObserver(chartEl, () => {
  setTimeout(() => chart?.resize(), 120);
});

watch(() => props.hostId, () => {
  selected.value = null;
  fetchData();
});

onMounted(async () => {
  initChart();
  await fetchData();
});

onBeforeUnmount(() => {
  chart?.dispose();
  chart = null;
});
</script>

<style scoped>
.topology-canvas {
  width: 100%;
  height: 560px;
  border: 1px solid rgba(128, 128, 128, 0.15);
  border-radius: 4px;
  background: var(--n-color, transparent);
}
</style>
