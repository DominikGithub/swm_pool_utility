<template>
  <!-- Distribution of one pool's open half-hour cells (% full), drawn at the
       slot's real pixel width and the height of a heatmap, so it can stand in
       for a heatmap without moving the layout. -->
  <div ref="root" class="pool-histogram" :style="{ height: `${height}px` }">
    <svg
      v-if="width > 0 && values.length"
      :width="width"
      :height="height"
      :viewBox="`0 0 ${width} ${height}`"
      role="img"
      :aria-label="`Distribution of ${values.length} half-hour slots, median ${fmt(stats.median)}% full`"
    >
      <template v-if="clipped">
        <rect class="tail" :x="X(0)" :y="PAD.t" :width="Math.max(0, X(stats.lo) - X(0))" :height="plotH" />
        <rect class="tail" :x="X(stats.hi)" :y="PAD.t" :width="Math.max(0, X(domainMax) - X(stats.hi))" :height="plotH" />
      </template>
      <rect
        v-for="bar in bars"
        :key="bar.i"
        :class="`bar level-${bar.level}`"
        :x="bar.x"
        :y="bar.y"
        :width="bar.w"
        :height="bar.h"
      />
      <line class="axis" :x1="PAD.l" :x2="width - PAD.r" :y1="baseY" :y2="baseY" />
      <text v-for="t in xTicks" :key="t" class="tick" :x="X(t)" :y="baseY + 11" text-anchor="middle">{{ t }}</text>
      <text class="tick" :x="width - PAD.r" :y="height - 1" text-anchor="end">% full</text>
      <text class="tick" :x="PAD.l - 4" :y="PAD.t + 7" text-anchor="end">{{ maxCount }}</text>
      <text class="tick" :x="PAD.l - 4" :y="baseY" text-anchor="end">0</text>
      <g v-for="m in markers" :key="m.label">
        <line :class="['marker', m.cls]" :x1="X(m.v)" :x2="X(m.v)" :y1="PAD.t - 3" :y2="baseY" />
        <text :class="['marker-label', m.cls]" :x="X(m.v)" :y="PAD.t - 5" text-anchor="middle">{{ m.label }}</text>
      </g>
    </svg>
    <p v-else-if="!values.length" class="hist-empty">No open readings in this period</p>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const props = defineProps({
  values: { type: Array, required: true }, // sorted occupancy of the open cells, % full
  stats: { type: Object, default: null }, // { lo, hi, median } — p5, p95, median
  clipped: { type: Boolean, default: false }, // per-pool scale: shade the clipped tails, mark p5/p95
  level: { type: Function, required: true }, // occupancy → colour level 0..5 of the active scale
  height: { type: Number, default: 140 },
})

const PAD = { l: 26, r: 8, t: 14, b: 16 }
const fmt = v => String(Math.round(v * 10) / 10)

const root = ref(null)
const width = ref(0)
let observer
onMounted(() => {
  observer = new ResizeObserver(([entry]) => { width.value = Math.floor(entry.contentRect.width) })
  observer.observe(root.value)
})
onBeforeUnmount(() => observer?.disconnect())

const top = computed(() => Math.max(1, props.values.at(-1) ?? 1))
const binWidth = computed(() => (top.value <= 20 ? 1 : top.value <= 50 ? 2 : 2.5))
const domainMax = computed(() => Math.ceil((top.value + 1e-9) / binWidth.value) * binWidth.value)
const counts = computed(() => {
  const n = Math.round(domainMax.value / binWidth.value)
  const c = Array(n).fill(0)
  for (const v of props.values) c[Math.min(n - 1, Math.floor(v / binWidth.value))]++
  return c
})
const maxCount = computed(() => Math.max(1, ...counts.value))
const baseY = computed(() => props.height - PAD.b)
const plotH = computed(() => baseY.value - PAD.t)

function X(v) {
  return PAD.l + (v / domainMax.value) * (width.value - PAD.l - PAD.r)
}

const bars = computed(() => counts.value.flatMap((c, i) => {
  if (!c) return []
  const h = (c / maxCount.value) * plotH.value
  return [{
    i,
    x: X(i * binWidth.value) + 0.5,
    w: Math.max(1, X(binWidth.value) - X(0) - 1),
    y: baseY.value - h,
    h,
    level: props.level((i + 0.5) * binWidth.value),
  }]
}))

const xTicks = computed(() => {
  const step = domainMax.value <= 20 ? 2 : domainMax.value <= 50 ? 5 : 10
  return Array.from({ length: Math.floor(domainMax.value / step) + 1 }, (_, i) => i * step)
})

const markers = computed(() => {
  if (!props.stats) return []
  const m = [{ v: props.stats.median, label: 'median', cls: 'median' }]
  if (props.clipped) m.push({ v: props.stats.lo, label: 'p5', cls: 'clip' }, { v: props.stats.hi, label: 'p95', cls: 'clip' })
  return m
})
</script>

<style scoped>
.pool-histogram {
  width: 100%;
  position: relative;
}

svg {
  display: block;
  font-size: 9.5px;
}

.tail { fill: #9ca3af; opacity: 0.18; }
.axis { stroke: #9ca3af; }
.tick { fill: #666; }

.bar.level-0 { fill: #15803d; }
.bar.level-1 { fill: #22c55e; }
.bar.level-2 { fill: #86efac; }
.bar.level-3 { fill: #fde047; }
.bar.level-4 { fill: #fb923c; }
.bar.level-5 { fill: #ef4444; }

.marker { stroke-width: 1.5; }
.marker.clip { stroke: #0066cc; stroke-dasharray: 4 3; }
.marker.median { stroke: #333; }
.marker-label { font-weight: 700; }
.marker-label.clip { fill: #0066cc; }
.marker-label.median { fill: #333; }

.hist-empty {
  margin: 0;
  padding-top: 3rem;
  text-align: center;
  font-size: 0.75rem;
  color: #888;
}
</style>
