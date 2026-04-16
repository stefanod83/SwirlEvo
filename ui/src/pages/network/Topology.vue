<template>
  <n-space vertical :size="12">
    <n-alert type="info">
      {{ t('tips.topology') }}
    </n-alert>

    <n-space justify="space-between" align="center" wrap>
      <n-space :size="6" align="center">
        <n-tag :bordered="false" type="info">
          <template #icon><n-icon><server-outline /></n-icon></template>
          {{ t('objects.host', 1) }}
        </n-tag>
        <n-tag :bordered="false" type="success">
          <template #icon><n-icon><globe-outline /></n-icon></template>
          {{ t('objects.network', 1) }}
        </n-tag>
        <n-tag :bordered="false" :color="{ color: 'transparent', textColor: '#854d0e', borderColor: '#facc15' }">
          <template #icon><n-icon :color="'#ca8a04'"><cube-outline /></n-icon></template>
          {{ t('objects.container', 1) }}
        </n-tag>
        <n-tag :bordered="false" type="error">
          <template #icon><n-icon><alert-circle-outline /></n-icon></template>
          {{ t('fields.exposed') }}
        </n-tag>
      </n-space>

      <n-space :size="14" align="center">
        <n-select
          v-model:value="viewMode"
          :options="viewOptions"
          size="small"
          style="width: 180px"
        />
        <n-space :size="6" align="center">
          <n-switch v-model:value="showInactive" size="small" @update:value="fetchData" />
          <span class="toggle-label">{{ t('buttons.show_inactive') }}</span>
        </n-space>
        <n-space :size="6" align="center">
          <n-switch v-model:value="showUnused" size="small" />
          <span class="toggle-label">{{ t('buttons.show_unused') }}</span>
        </n-space>
        <n-button-group v-if="viewMode === 'sankey'" size="small">
          <n-button secondary @click="zoomOut" :disabled="sankeyZoom <= 0.6">
            <template #icon><n-icon><remove-outline /></n-icon></template>
          </n-button>
          <n-button secondary @click="resetZoom">{{ Math.round(sankeyZoom * 100) }}%</n-button>
          <n-button secondary @click="zoomIn" :disabled="sankeyZoom >= 3">
            <template #icon><n-icon><add-outline /></n-icon></template>
          </n-button>
        </n-button-group>
        <n-button secondary size="small" @click="downloadBlueprint">
          <template #icon><n-icon><download-outline /></n-icon></template>
          {{ t('buttons.download') }}
        </n-button>
        <n-button secondary size="small" @click="fetchData" :loading="loading">
          <template #icon><n-icon><refresh-outline /></n-icon></template>
          {{ t('buttons.refresh') || 'Refresh' }}
        </n-button>
      </n-space>
    </n-space>

    <div class="topology-viewport" :style="viewportStyle" @wheel="onWheel">
      <div ref="chartEl" class="topology-canvas" :style="canvasStyle" />
    </div>

    <n-card v-if="selected" size="small" :bordered="true">
      <template #header>
        <n-space :size="8" align="center" inline>
          <n-icon :component="nodeIcon(selected.type)" />
          <span>{{ selectedLabel }}</span>
          <n-tag size="small" :type="nodeTypeTag(selected.type)" :bordered="false">
            {{ t(`objects.${selected.type}`, 1) }}
          </n-tag>
        </n-space>
      </template>
      <template #header-extra>
        <n-button text size="small" @click="selected = null">
          <template #icon><n-icon><close-outline /></n-icon></template>
        </n-button>
      </template>

      <n-space vertical :size="10">
        <n-space v-if="selected.flags && selected.flags.length" :size="6">
          <n-tag
            v-for="f in selected.flags"
            :key="f"
            :type="flagTagType(f)"
            :bordered="false"
            size="small"
          >
            {{ flagLabel(f) }}
          </n-tag>
        </n-space>

        <template v-if="selected.type === 'host'">
          <n-descriptions :column="3" size="small" label-placement="top">
            <n-descriptions-item :label="t('fields.name')">
              <strong>{{ selectedLabel }}</strong>
            </n-descriptions-item>
            <n-descriptions-item :label="t('objects.network', 2)">
              {{ selected.meta?.networkCount ?? '—' }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('objects.container', 2)">
              <span v-if="selected.meta?.showInactive">{{ selected.meta?.includedCount }} / {{ selected.meta?.totalCount }}</span>
              <span v-else>{{ selected.meta?.runningCount ?? selected.meta?.includedCount }} {{ t('buttons.running_short') }}</span>
            </n-descriptions-item>
          </n-descriptions>
        </template>

        <template v-else-if="selected.type === 'network'">
          <n-descriptions :column="selected.meta?.stack ? 4 : 3" size="small" label-placement="top">
            <n-descriptions-item :label="t('fields.name')">
              <strong>{{ selectedNetworkLabel }}</strong>
            </n-descriptions-item>
            <n-descriptions-item v-if="selected.meta?.stack" :label="t('objects.stack', 1) || 'Stack'">
              <n-tag size="small" type="info" :bordered="false">{{ selected.meta.stack }}</n-tag>
            </n-descriptions-item>
            <n-descriptions-item :label="t('fields.driver')">
              {{ selected.meta?.driver || '—' }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('fields.scope')">
              {{ selected.meta?.scope || '—' }}
            </n-descriptions-item>
          </n-descriptions>
          <n-table v-if="ipamRows.length" size="small" :bordered="false" :single-line="false">
            <thead>
              <tr>
                <th>{{ t('fields.subnet') }}</th>
                <th>{{ t('fields.gateway') }}</th>
                <th>{{ t('fields.range') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in ipamRows" :key="i">
                <td>{{ row.subnet || '—' }}</td>
                <td>{{ row.gateway || '—' }}</td>
                <td>{{ row.range || '—' }}</td>
              </tr>
            </tbody>
          </n-table>
        </template>

        <template v-else-if="selected.type === 'container'">
          <n-descriptions :column="selected.meta?.stack ? 4 : 3" size="small" label-placement="top">
            <n-descriptions-item :label="t('fields.name')">
              <strong>{{ selected.meta?.name || selected.label }}</strong>
            </n-descriptions-item>
            <n-descriptions-item v-if="selected.meta?.stack" :label="t('objects.stack', 1) || 'Stack'">
              <n-tag size="small" type="info" :bordered="false">{{ selected.meta.stack }}</n-tag>
            </n-descriptions-item>
            <n-descriptions-item :label="t('fields.state')">
              <n-tag size="small" :type="selected.meta?.state === 'running' ? 'success' : 'default'" :bordered="false">
                {{ selected.meta?.state || '—' }}
              </n-tag>
            </n-descriptions-item>
            <n-descriptions-item :label="t('fields.image') || 'Image'">
              <span style="font-family: monospace; font-size: 11px">{{ selected.meta?.image || '—' }}</span>
            </n-descriptions-item>
          </n-descriptions>
          <n-table v-if="networkRows.length" size="small" :bordered="false" :single-line="false">
            <thead>
              <tr>
                <th>{{ t('objects.network', 1) }}</th>
                <th>IPv4</th>
                <th>IPv6</th>
                <th>{{ t('fields.mac') || 'MAC' }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in networkRows" :key="i">
                <td>{{ row.name }}</td>
                <td><span style="font-family: monospace; font-size: 11px">{{ row.ip || '—' }}</span></td>
                <td><span style="font-family: monospace; font-size: 11px">{{ row.ipv6 || '—' }}</span></td>
                <td><span style="font-family: monospace; font-size: 11px">{{ row.mac || '—' }}</span></td>
              </tr>
            </tbody>
          </n-table>
          <n-table v-if="portRows.length" size="small" :bordered="false" :single-line="false">
            <thead>
              <tr>
                <th>{{ t('fields.host') || 'Host IP' }}</th>
                <th>{{ t('fields.public_port') }}</th>
                <th>{{ t('fields.private_port') }}</th>
                <th>{{ t('fields.protocol') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in portRows" :key="i">
                <td>{{ row.ip || '0.0.0.0' }}</td>
                <td><strong>{{ row.publicPort }}</strong></td>
                <td>{{ row.privatePort }}</td>
                <td>{{ row.type }}</td>
              </tr>
            </tbody>
          </n-table>
        </template>
      </n-space>
    </n-card>
  </n-space>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import * as echarts from "echarts";
import {
  NSpace, NButton, NButtonGroup, NIcon, NCard, NTag, NAlert,
  NDescriptions, NDescriptionsItem, NSwitch, NTable, NSelect,
  useMessage,
} from "naive-ui";
import {
  RefreshOutline, CloseOutline, DownloadOutline,
  AddOutline, RemoveOutline,
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
const showInactive = ref(false);
const showUnused = ref(false);

// Only two graph layouts + Sankey for dependency flow. Circular is the
// default — it's the most readable starting point and the force simulation
// spreads things apart from there.
type ViewMode = "force" | "circular" | "sankey";
const viewMode = ref<ViewMode>("circular");
const viewOptions = computed(() => [
  {
    type: "group",
    label: t('fields.layout_graph_group'),
    key: "graph",
    children: [
      { label: t('fields.layout_circular'), value: "circular" },
      { label: t('fields.layout_force'), value: "force" },
    ],
  },
  {
    type: "group",
    label: t('fields.layout_hier_group'),
    key: "hier",
    children: [
      { label: t('fields.layout_sankey'), value: "sankey" },
    ],
  },
]);

let requestGen = 0;
let lastTopology: NetworkTopology | null = null;

// Sankey doesn't support roam/zoom natively. We fake it by growing the canvas
// inside a scrolling viewport. Graph modes already handle wheel zoom via
// series.roam=true, so we keep sankeyZoom at 1 for them.
const sankeyZoom = ref(1);
const VIEWPORT_HEIGHT = 620;

const viewportStyle = computed(() => ({
  height: VIEWPORT_HEIGHT + "px",
  overflow: viewMode.value === "sankey" ? "auto" : "hidden",
}));

const canvasStyle = computed(() => {
  if (viewMode.value === "sankey") {
    const z = sankeyZoom.value;
    return {
      width: (100 * z) + "%",
      height: (VIEWPORT_HEIGHT * z) + "px",
      minWidth: (100 * z) + "%",
    } as Record<string, string>;
  }
  return { width: "100%", height: VIEWPORT_HEIGHT + "px" } as Record<string, string>;
});

function zoomIn() {
  sankeyZoom.value = Math.min(3, +(sankeyZoom.value + 0.2).toFixed(2));
}
function zoomOut() {
  sankeyZoom.value = Math.max(0.6, +(sankeyZoom.value - 0.2).toFixed(2));
}
function resetZoom() {
  sankeyZoom.value = 1;
}

function onWheel(e: WheelEvent) {
  if (viewMode.value !== "sankey" || !e.ctrlKey) return;
  // Ctrl+wheel zooms the sankey; plain wheel scrolls the viewport.
  e.preventDefault();
  if (e.deltaY < 0) zoomIn();
  else zoomOut();
}

watch(sankeyZoom, () => {
  // Give the DOM one tick to apply the new size before telling ECharts to
  // resize its internal canvas accordingly.
  setTimeout(() => chart?.resize(), 40);
});
watch(viewMode, () => {
  if (viewMode.value !== "sankey") sankeyZoom.value = 1;
});

const hostName = computed(() => {
  if (!props.hostId) return "";
  const h = (store.state.hosts || []).find((x: any) => x.id === props.hostId);
  return h?.name || props.hostId;
});

const selectedLabel = computed(() => {
  if (!selected.value) return "";
  if (selected.value.type === "host") return hostName.value || selected.value.label;
  return selected.value.label;
});

const ipamRows = computed<Array<{ subnet?: string; gateway?: string; range?: string }>>(() => {
  const raw = selected.value?.meta?.ipam;
  if (!Array.isArray(raw)) return [];
  return raw;
});

const portRows = computed<Array<{ ip?: string; publicPort?: number; privatePort?: number; type?: string }>>(() => {
  const raw = selected.value?.meta?.ports;
  if (!Array.isArray(raw)) return [];
  return raw;
});

const networkRows = computed<Array<{ name: string; ip?: string; ipv6?: string; mac?: string }>>(() => {
  const raw = selected.value?.meta?.networks;
  if (!Array.isArray(raw)) return [];
  return raw;
});

const selectedNetworkLabel = computed(() => {
  if (!selected.value) return "";
  // Use the same stack-stripped label the graph shows.
  return labelFor(selected.value);
});

function nodeIcon(type: string) {
  if (type === "host") return ServerOutline;
  if (type === "network") return GlobeOutline;
  return CubeOutline;
}

function nodeTypeTag(type: string): "info" | "success" | "warning" | "default" {
  if (type === "host") return "info";
  if (type === "network") return "success";
  if (type === "container") return "warning";
  return "default";
}

function flagLabel(f: string): string {
  switch (f) {
    case "exposed-public": return t("fields.exposed");
    case "exposed-direct": return t("fields.exposed_direct");
    case "local-only": return t("fields.local_only");
    case "isolated": return t("fields.isolated");
    case "ingress": return t("fields.ingress");
    case "unused": return t("fields.unused");
    case "inactive": return t("fields.inactive");
    default: return f;
  }
}

function flagTagType(f: string): "error" | "warning" | "info" | "success" | "default" {
  switch (f) {
    case "exposed-public":
    case "exposed-direct":
      return "error";
    case "local-only": return "success";
    case "isolated": return "info";
    case "ingress": return "warning";
    case "unused": return "warning";
    case "inactive": return "default";
    default: return "default";
  }
}

function labelFor(n: NetworkTopologyNode): string {
  if (n.type === "host") return hostName.value || n.label || "host";
  const stack = n.meta?.stack as string | undefined;
  if (stack && n.label) {
    // Compose networks are conventionally named "<project>_<name>" (legacy
    // engine) or just "<name>" (newer). Container names follow
    // "<project>-<service>-<N>" or the underscore variant. Strip whichever
    // prefix matches so the node label is the short, readable piece only.
    const prefixCandidates = [stack + "_", stack + "-"];
    for (const p of prefixCandidates) {
      if (n.label.startsWith(p)) return n.label.substring(p.length);
    }
  }
  return n.label;
}

function applyFilter(topo: NetworkTopology): NetworkTopology {
  if (showUnused.value) return topo;
  const drop = new Set(
    topo.nodes.filter(n => n.flags?.includes("unused")).map(n => n.id),
  );
  if (!drop.size) return topo;
  return {
    hostId: topo.hostId,
    nodes: topo.nodes.filter(n => !drop.has(n.id)),
    edges: topo.edges.filter(e => !drop.has(e.source) && !drop.has(e.target)),
  };
}

// --- Graph (force / circular) ---------------------------------------------

function buildGraphOption(topo: NetworkTopology, mode: "force" | "circular"): echarts.EChartsOption {
  const nodes = topo.nodes;
  const hostNodeId = nodes.find(n => n.type === "host")?.id;

  const networkNodes = nodes.filter(n => n.type === "network");
  const containerByNet: Record<string, string[]> = {};
  const seenCtInNet = new Set<string>();
  for (const e of topo.edges) {
    if (e.type === "network-container") {
      (containerByNet[e.source] ||= []).push(e.target);
      seenCtInNet.add(e.target);
    }
  }
  const containerNoNet = nodes
    .filter(n => n.type === "container" && !seenCtInNet.has(n.id))
    .map(n => n.id);

  const totalContainers = nodes.filter(n => n.type === "container").length;
  const netCount = Math.max(networkNodes.length, 1);

  // Seed positions for Circular mode. Force mode lets ECharts choose freely.
  const positions: Record<string, { x: number; y: number }> = {};
  if (hostNodeId) positions[hostNodeId] = { x: 0, y: 0 };

  if (mode === "circular") {
    const netRadius = 160 + Math.max(networkNodes.length, 1) * 22;
    networkNodes.forEach((nn, i) => {
      const angle = (2 * Math.PI * i) / netCount - Math.PI / 2;
      positions[nn.id] = {
        x: Math.cos(angle) * netRadius,
        y: Math.sin(angle) * netRadius,
      };
      const ctrs = containerByNet[nn.id] || [];
      if (!ctrs.length) return;
      const clusterR = 28 + Math.sqrt(ctrs.length) * 28;
      ctrs.forEach((cid, j) => {
        const total = ctrs.length;
        const arcSpan = Math.min(Math.PI, 0.35 + ctrs.length * 0.18);
        let inner = 0;
        if (total > 1) inner = arcSpan * (j / (total - 1) - 0.5);
        const a = angle + inner;
        positions[cid] = {
          x: Math.cos(angle) * netRadius + Math.cos(a) * clusterR,
          y: Math.sin(angle) * netRadius + Math.sin(a) * clusterR,
        };
      });
    });
    containerNoNet.forEach((cid, i) => {
      const angle = (2 * Math.PI * i) / Math.max(containerNoNet.length, 1) - Math.PI / 2;
      const r = 90 + Math.sqrt(containerNoNet.length) * 18;
      positions[cid] = { x: Math.cos(angle) * r, y: Math.sin(angle) * r };
    });
  }

  const data = nodes.map((n: NetworkTopologyNode) => {
    const exposed = n.flags?.includes("exposed-public");
    const exposedDirect = n.flags?.includes("exposed-direct");
    const isolated = n.flags?.includes("isolated");
    const unused = n.flags?.includes("unused");
    const inactive = n.flags?.includes("inactive");
    const category = exposed ? 3 : n.type === "host" ? 0 : n.type === "network" ? 1 : 2;
    const symbolSize = n.type === "host" ? 64 : n.type === "network" ? 38 : 26;
    const symbol = n.type === "host" ? "roundRect" : n.type === "network" ? "diamond" : "circle";
    const itemStyle: any = {};
    if (n.type === "host") {
      itemStyle.borderColor = "#1d4ed8";
      itemStyle.borderWidth = 3;
      itemStyle.shadowBlur = 8;
      itemStyle.shadowColor = "rgba(59, 130, 246, 0.45)";
    }
    if (isolated) {
      itemStyle.borderColor = "#3b82f6";
      itemStyle.borderWidth = 3.5;
    }
    if (exposedDirect) {
      itemStyle.borderColor = "#dc2626";
      itemStyle.borderWidth = 3;
      itemStyle.borderType = "solid";
    }
    if (unused) {
      itemStyle.opacity = 0.55;
      itemStyle.borderColor = "#f59e0b";
      itemStyle.borderType = "dashed";
      itemStyle.borderWidth = 2;
    }
    if (inactive) {
      itemStyle.opacity = 0.4;
      itemStyle.color = "#9ca3af";
    }
    const node: any = {
      id: n.id,
      name: labelFor(n),
      category,
      symbol,
      symbolSize,
      itemStyle: Object.keys(itemStyle).length ? itemStyle : undefined,
      label: inactive ? { color: "#9ca3af" } : undefined,
      value: { meta: n.meta, type: n.type, flags: n.flags, label: labelFor(n), rawLabel: n.label },
    };
    if (positions[n.id]) {
      node.x = positions[n.id].x;
      node.y = positions[n.id].y;
    }
    if (n.id === hostNodeId) {
      node.fixed = true;
      node.x = 0;
      node.y = 0;
    }
    return node;
  });

  const inactiveSet = new Set(
    nodes.filter(n => n.flags?.includes("inactive")).map(n => n.id),
  );
  const nameById: Record<string, string> = {};
  nodes.forEach(n => { nameById[n.id] = labelFor(n); });

  const links = topo.edges.map((e: NetworkTopologyEdge) => {
    const touchesInactive = inactiveSet.has(e.source) || inactiveSet.has(e.target);
    return {
      source: e.source,
      target: e.target,
      lineStyle: {
        type: e.type === "host-network" ? "dashed" : "solid",
        width: e.type === "network-container" ? 1.2 : 1,
        opacity: touchesInactive ? 0.3 : 0.7,
        color: touchesInactive ? "#9ca3af" : undefined,
      },
      label: e.label ? { show: true, formatter: e.label, fontSize: 10 } : undefined,
      value: {
        edgeType: e.type,
        sourceName: nameById[e.source] || e.source,
        targetName: nameById[e.target] || e.target,
        edgeLabel: e.label || "",
      },
    };
  });

  return {
    tooltip: { trigger: "item", formatter: genericFormatter },
    legend: { show: false },
    animation: true,
    series: [
      {
        type: "graph",
        // Always force-simulated. Circular mode just seeds positions before
        // the simulation runs; Force starts from scratch.
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
          repulsion: mode === "force"
            ? 280 + Math.max(0, totalContainers - 10) * 8
            : 520 + Math.max(0, totalContainers - 5) * 14,
          gravity: mode === "force" ? 0.18 : 0.04,
          edgeLength: mode === "force" ? [60, 140] : [90, 180],
          friction: 0.4,
          layoutAnimation: true,
        },
        label: { show: true, position: "right", fontSize: 11 },
        // Mouseover-only emphasis. No persistent selection state — it would
        // fight with the network border colors for visual priority.
        emphasis: { focus: "adjacency", lineStyle: { width: 2 } },
        data,
        links,
      },
    ],
    color: ["#3b82f6", "#10b981", "#facc15", "#dc2626"],
  } as echarts.EChartsOption;
}

// --- Sankey variant --------------------------------------------------------

function buildSankeyOption(topo: NetworkTopology): echarts.EChartsOption {
  const nodes = topo.nodes;
  const sankeyNodes = nodes.map((n) => {
    return {
      name: n.id,
      label: { formatter: labelFor(n), fontSize: 11 },
      itemStyle: nodeStyle(n),
      value: { meta: n.meta, type: n.type, flags: n.flags || [], label: labelFor(n), rawLabel: n.label, id: n.id },
    };
  });
  const sankeyLinks = topo.edges.map(e => ({
    source: e.source,
    target: e.target,
    value: 1,
    lineStyle: { color: "gradient", opacity: 0.35 },
    value2: {
      edgeType: e.type,
      sourceName: nodes.find(n => n.id === e.source)?.label || e.source,
      targetName: nodes.find(n => n.id === e.target)?.label || e.target,
      edgeLabel: e.label || "",
    },
  }));
  return {
    tooltip: {
      trigger: "item",
      formatter: (p: any) => {
        if (p.dataType === "edge" && p.data?.value2) {
          const v = p.data.value2;
          return `<b>${v.sourceName}</b> → <b>${v.targetName}</b>${v.edgeLabel ? `<br/><small>${v.edgeLabel}</small>` : ""}`;
        }
        if (p.dataType === "node" && p.data?.value) {
          return nodeTooltip(p.data.value);
        }
        return p.name || "";
      },
    },
    series: [
      {
        type: "sankey",
        data: sankeyNodes,
        links: sankeyLinks,
        emphasis: { focus: "adjacency" },
        nodeAlign: "left",
        nodeWidth: 18,
        nodeGap: 10,
        top: "2%",
        bottom: "4%",
        left: "4%",
        right: "12%",
        label: { formatter: (p: any) => p.data?.label?.formatter || p.name, fontSize: 11 },
        layoutIterations: 64,
      } as any,
    ],
  } as echarts.EChartsOption;
}

function nodeStyle(n: NetworkTopologyNode): any {
  const exposed = n.flags?.includes("exposed-public");
  const exposedDirect = n.flags?.includes("exposed-direct");
  const isolated = n.flags?.includes("isolated");
  const inactive = n.flags?.includes("inactive");
  const base: any = {};
  if (n.type === "host") base.color = "#3b82f6";
  else if (n.type === "network") base.color = "#10b981";
  else base.color = exposed ? "#dc2626" : "#facc15";
  if (isolated) { base.borderColor = "#3b82f6"; base.borderWidth = 3; }
  if (exposedDirect) { base.borderColor = "#dc2626"; base.borderWidth = 3; base.borderType = "solid"; }
  if (inactive) base.opacity = 0.4;
  return base;
}

// --- Shared tooltip formatter ---------------------------------------------

function genericFormatter(p: any): string {
  if (p.dataType === "edge" && p.data?.value?.edgeType) {
    const v = p.data.value;
    const base = `<b>${v.sourceName}</b> → <b>${v.targetName}</b>`;
    return v.edgeLabel ? `${base}<br/><small>${v.edgeLabel}</small>` : base;
  }
  if (!p.data?.value) return p.name || "";
  return nodeTooltip(p.data.value);
}

interface HierValue { meta: any; type: string; flags: string[]; label: string; rawLabel: string; id: string }

function nodeTooltip(v: HierValue): string {
  const type = v.type as string;
  const parts: string[] = [`<b>${v.label}</b> <small>(${type})</small>`];
  if (v.flags?.length) {
    parts.push(
      v.flags
        .map((f: string) => `<span style="color:#ef4444">${flagLabel(f)}</span>`)
        .join(" · "),
    );
  }
  const meta = v.meta || {};
  if (type === "network") {
    if (meta.driver) parts.push(`driver: ${meta.driver}`);
    if (meta.scope) parts.push(`scope: ${meta.scope}`);
    if (meta.ipam?.length) {
      const first = meta.ipam[0];
      if (first.subnet) parts.push(`subnet: ${first.subnet}`);
    }
  } else if (type === "container") {
    if (meta.state) parts.push(`state: ${meta.state}`);
    if (meta.image) parts.push(`image: ${meta.image}`);
    if (meta.ports?.length) {
      const ports = meta.ports
        .map((p: any) => `${p.ip || "0.0.0.0"}:${p.publicPort}→${p.privatePort}/${p.type}`)
        .join("<br/>");
      parts.push(`ports:<br/>${ports}`);
    }
  } else if (type === "host") {
    if (meta.networkCount !== undefined) parts.push(`networks: ${meta.networkCount}`);
    if (meta.showInactive) parts.push(`containers: ${meta.includedCount} / ${meta.totalCount}`);
    else if (meta.runningCount !== undefined) parts.push(`containers: ${meta.runningCount} running`);
  }
  return parts.join("<br/>");
}

// --- Rendering -------------------------------------------------------------

function buildOption(topo: NetworkTopology): echarts.EChartsOption {
  const mode = viewMode.value;
  if (mode === "sankey") return buildSankeyOption(topo);
  return buildGraphOption(topo, mode);
}

function render() {
  if (!chart || !lastTopology) return;
  const filtered = applyFilter(lastTopology);
  chart.setOption(buildOption(filtered), true);

  const stillPresent = selected.value && filtered.nodes.some(n => n.id === selected.value!.id);
  if (!stillPresent) {
    const host = filtered.nodes.find(n => n.type === "host");
    if (host) {
      selected.value = {
        id: host.id,
        type: "host",
        label: labelFor(host),
        meta: host.meta,
        flags: host.flags,
      };
    } else {
      selected.value = null;
    }
  }
}

watch(showUnused, () => render());
watch(viewMode, () => render());

async function fetchData() {
  if (!props.hostId) {
    chart?.clear();
    lastTopology = null;
    selected.value = null;
    return;
  }
  const myGen = ++requestGen;
  loading.value = true;
  try {
    const r = await networkApi.topology(props.hostId, showInactive.value);
    if (myGen !== requestGen) return;
    lastTopology = r.data as NetworkTopology;
    render();
  } catch (e: any) {
    if (myGen !== requestGen) return;
    message.error(e?.message || String(e));
  } finally {
    if (myGen === requestGen) loading.value = false;
  }
}

function downloadBlueprint() {
  if (!chart) return;
  const bg = store.state.preference?.theme === "dark" ? "#18181c" : "#ffffff";
  const url = chart.getDataURL({
    type: "png",
    pixelRatio: 2,           // higher DPI for a printable blueprint
    backgroundColor: bg,
    excludeComponents: ["toolbox", "dataZoom"],
  });
  const hostLabel = hostName.value || props.hostId || "topology";
  const stamp = new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19);
  const filename = `swirl-topology-${hostLabel}-${viewMode.value}-${stamp}.png`;
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
}

function initChart() {
  if (!chartEl.value) return;
  const theme = store.state.preference?.theme || "light";
  chart = echarts.init(chartEl.value, theme);
  chart.on("click", (params: any) => {
    if (params.dataType === "edge") return;
    const n = params.data;
    if (!n) return;
    const payload = (n.value && typeof n.value === "object") ? n.value : null;
    if (!payload || !payload.type) return;
    selected.value = {
      id: (n.id as string) || (n.name as string),
      type: payload.type,
      label: payload.rawLabel || payload.label,
      meta: payload.meta,
      flags: payload.flags,
    };
  });
}

useResizeObserver(chartEl, () => {
  setTimeout(() => chart?.resize(), 120);
});

watch(() => props.hostId, () => {
  selected.value = null;
  lastTopology = null;
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
.topology-viewport {
  width: 100%;
  border: 1px solid rgba(128, 128, 128, 0.15);
  border-radius: 4px;
  background: var(--n-color, transparent);
}
.topology-canvas {
  width: 100%;
  height: 620px;
}
.toggle-label {
  font-size: 12px;
  color: var(--n-text-color-3, #666);
}
</style>
