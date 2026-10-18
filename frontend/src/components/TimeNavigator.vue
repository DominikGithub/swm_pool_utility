<template>
  <div class="time-nav">
    <!-- Only for a past window; the strip sits at the bottom, so this row
         appearing shrinks the chart rather than moving the strip. -->
    <div v-if="isPast" class="nav-head">
      <span class="nav-range">{{ rangeText }}</span>
      <button type="button" class="nav-now" @click="commit(null)">↺ Now</button>
    </div>
    <div
      ref="stripRef"
      class="nav-strip"
      tabindex="0"
      role="slider"
      aria-label="Time window"
      :aria-valuemin="minEnd"
      :aria-valuemax="days.length"
      :aria-valuenow="endIdx ?? days.length"
      :aria-valuetext="isPast ? rangeText : 'Now'"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerUp"
      @pointercancel="onPointerCancel"
      @keydown="onKeyDown"
    >
      <svg v-if="width > 0" :width="width" :height="HEIGHT" aria-hidden="true">
        <line
          v-for="m in months" :key="'t' + m.x"
          class="month-tick" :x1="m.x" :x2="m.x" y1="0" :y2="BARS_H"
        />
        <rect
          v-for="b in bars" :key="b.i"
          :class="b.inWindow ? 'bar in' : 'bar'"
          :x="b.x" :y="b.y" :width="b.w" :height="b.h"
        />
        <rect class="window" :x="win.x" y="0.75" :width="win.w" :height="BARS_H - 0.5" rx="3" />
        <text
          v-for="m in months" :key="'l' + m.x"
          class="month-label" :x="m.x + 3" :y="HEIGHT - 3"
        >{{ m.label }}</text>
      </svg>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'

// A strip with one bar per day (that day's peak utilization) over the whole
// history, and the chart's time window drawn on top of it.  Dragging or
// tapping moves the window into the past; past windows cover whole Berlin
// calendar days.  Reaching today switches back to the live window.
const props = defineProps({
  // Berlin midnight of each day, RFC3339, from the first reading up to today.
  days: { type: Array, required: true },
  // Daily peak (0–100) per day, or null for a day without readings.
  values: { type: Array, required: true },
  // Window width in days (the range dropdown).
  rangeDays: { type: Number, required: true },
  // End of a past window (ms of a day start, exclusive); null = live, ending now.
  windowEnd: { type: Number, default: null }
})
const emit = defineEmits(['update:windowEnd'])

const DAY_MS = 864e5
const BARS_H = 34
const HEIGHT = 50
const MIN_WINDOW_PX = 10
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

const stripRef = ref(null)
const width = ref(0)
const nowMs = ref(Date.now())
// Window end while dragging, as an index into days (exclusive end) or null
// for live; undefined when not dragging.
const previewIdx = ref(undefined)
let drag = null

const dayStarts = computed(() => props.days.map(d => Date.parse(d)))
const minEnd = computed(() => Math.min(props.rangeDays, props.days.length - 1))
const committedIdx = computed(() => {
  if (props.windowEnd == null) return null
  const i = dayStarts.value.indexOf(props.windowEnd)
  return i >= 0 ? i : null
})
const endIdx = computed(() => (previewIdx.value !== undefined ? previewIdx.value : committedIdx.value))
const isPast = computed(() => endIdx.value != null)

function xOf(t) {
  const span = nowMs.value - dayStarts.value[0]
  return span > 0 ? ((t - dayStarts.value[0]) / span) * width.value : 0
}

function tOf(x) {
  const clamped = Math.max(0, Math.min(width.value, x))
  return dayStarts.value[0] + (clamped / width.value) * (nowMs.value - dayStarts.value[0])
}

function bounds(idx) {
  if (idx == null) return [nowMs.value - props.rangeDays * DAY_MS, nowMs.value]
  return [dayStarts.value[Math.max(0, idx - props.rangeDays)], dayStarts.value[idx]]
}

// Window end for a window centred on centerMs, snapped to whole days.
function idxForCenter(centerMs) {
  const idx = Math.round((centerMs - dayStarts.value[0]) / DAY_MS + props.rangeDays / 2)
  if (idx >= props.days.length) return null
  return Math.max(minEnd.value, idx)
}

const win = computed(() => {
  const [a, b] = bounds(endIdx.value)
  let x0 = Math.max(0, xOf(a))
  let x1 = Math.min(width.value, xOf(b))
  if (x1 - x0 < MIN_WINDOW_PX) {
    const c = (x0 + x1) / 2
    x0 = Math.max(0, Math.min(width.value - MIN_WINDOW_PX, c - MIN_WINDOW_PX / 2))
    x1 = x0 + MIN_WINDOW_PX
  }
  return { x: x0, w: x1 - x0 }
})

const bars = computed(() => {
  if (!width.value) return []
  const [a, b] = bounds(endIdx.value)
  return dayStarts.value.flatMap((start, i) => {
    const v = props.values[i]
    if (v == null) return []
    const end = dayStarts.value[i + 1] ?? nowMs.value
    const x = xOf(start)
    const w = xOf(end) - x
    const h = Math.max(1, (Math.min(100, v) / 100) * (BARS_H - 2))
    return [{ i, x, w: Math.max(0.5, w - (w > 3 ? 1 : 0)), y: BARS_H - h, h, inWindow: end > a && start < b }]
  })
})

// Month labels at each month start; the first (partial) month only when
// there is room before the next label.
const months = computed(() => {
  if (!width.value) return []
  const out = []
  props.days.forEach((d, i) => {
    if (d.slice(8, 10) === '01') out.push({ x: xOf(dayStarts.value[i]), label: MONTHS[Number(d.slice(5, 7)) - 1] })
  })
  const first = props.days[0]
  if (first && first.slice(8, 10) !== '01' && (out.length === 0 || out[0].x >= 28)) {
    out.unshift({ x: 0, label: MONTHS[Number(first.slice(5, 7)) - 1] })
  }
  const kept = []
  for (const m of out) {
    if (!kept.length || m.x - kept[kept.length - 1].x >= 24) kept.push(m)
  }
  return kept
})

const fmtDay = new Intl.DateTimeFormat('en-GB', {
  timeZone: 'Europe/Berlin', weekday: 'short', day: 'numeric', month: 'short', year: 'numeric'
})
const fmtStart = new Intl.DateTimeFormat('en-GB', { timeZone: 'Europe/Berlin', day: 'numeric', month: 'short' })
const fmtEnd = new Intl.DateTimeFormat('en-GB', { timeZone: 'Europe/Berlin', day: 'numeric', month: 'short', year: 'numeric' })

const rangeText = computed(() => {
  if (endIdx.value == null) return ''
  const [a, b] = bounds(endIdx.value)
  if (props.rangeDays === 1) return fmtDay.format(a)
  return `${fmtStart.format(a)} – ${fmtEnd.format(b - 1)}`
})

function commit(idx) {
  const end = idx == null ? null : dayStarts.value[idx]
  if (end !== props.windowEnd) emit('update:windowEnd', end)
}

function pointerX(e) {
  return e.clientX - stripRef.value.getBoundingClientRect().left
}

function onPointerDown(e) {
  if (e.button > 0) return
  nowMs.value = Date.now()
  const x = pointerX(e)
  const w = win.value
  const [a, b] = bounds(endIdx.value)
  // Grabbing the window keeps its offset; anywhere else jumps the window there.
  const grabbed = x >= w.x - 4 && x <= w.x + w.w + 4
  drag = { id: e.pointerId, offsetMs: grabbed ? tOf(x) - (a + b) / 2 : 0 }
  stripRef.value.setPointerCapture(e.pointerId)
  previewIdx.value = idxForCenter(tOf(x) - drag.offsetMs)
}

function onPointerMove(e) {
  if (!drag || e.pointerId !== drag.id) return
  previewIdx.value = idxForCenter(tOf(pointerX(e)) - drag.offsetMs)
}

function onPointerUp(e) {
  if (!drag || e.pointerId !== drag.id) return
  const idx = previewIdx.value
  drag = null
  previewIdx.value = undefined
  commit(idx)
}

// The browser took over (e.g. a vertical scroll started on the strip).
function onPointerCancel() {
  drag = null
  previewIdx.value = undefined
}

function onKeyDown(e) {
  const n = props.days.length
  const cur = committedIdx.value ?? n
  let next
  if (e.key === 'ArrowLeft') next = Math.max(minEnd.value, cur - props.rangeDays)
  else if (e.key === 'ArrowRight') next = cur + props.rangeDays >= n ? null : cur + props.rangeDays
  else if (e.key === 'Home') next = minEnd.value
  else if (e.key === 'End') next = null
  else return
  e.preventDefault()
  commit(next)
}

watch(() => props.days, () => { nowMs.value = Date.now() })

let resizeObserver = null
onMounted(() => {
  resizeObserver = new ResizeObserver(entries => {
    width.value = Math.floor(entries[0].contentRect.width)
  })
  resizeObserver.observe(stripRef.value)
})
onBeforeUnmount(() => resizeObserver?.disconnect())
</script>

<style scoped>
.time-nav {
  flex-shrink: 0;
  padding: 0.3rem 0.9rem 0.45rem;
  border-top: 1px solid #eef1f5;
  user-select: none;
}

.nav-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  min-height: 1.6rem;
  font-size: 0.75rem;
}

.nav-range {
  font-weight: 600;
  color: #1f2937;
}

/* Overrides the global button style (large, uppercase, blue). */
.nav-now {
  height: auto;
  padding: 0.15rem 0.65rem;
  background: white;
  color: #0066cc;
  border: 1px solid #0066cc;
  border-radius: 999px;
  box-shadow: none;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: none;
  letter-spacing: normal;
}

.nav-now:hover {
  transform: none;
  box-shadow: none;
  background: #eef5fc;
}

.nav-strip {
  height: 50px;
  cursor: pointer;
  touch-action: pan-y;
  border-radius: 4px;
  outline: none;
}

.nav-strip:focus-visible {
  box-shadow: 0 0 0 2px rgba(0, 102, 204, 0.35);
}

.nav-strip svg {
  display: block;
}

.bar {
  fill: #cbd5e1;
}

.bar.in {
  fill: #6b9fd8;
}

.window {
  fill: rgba(0, 102, 204, 0.1);
  stroke: #0066cc;
  stroke-width: 1.5;
}

.month-tick {
  stroke: #e5e7eb;
}

.month-label {
  font-size: 10px;
  fill: #6b7280;
}
</style>
