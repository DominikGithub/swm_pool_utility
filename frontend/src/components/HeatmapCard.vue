<template>
  <div class="heatmap-container">
    <div class="heatmap-grid" :class="{ 'single-pool': poolDataList.length === 1 }">
      <div v-for="poolData in poolDataList" :key="poolData.pool" class="pool-heatmap">
        <div class="pool-header">{{ poolData.pool }}</div>
        <div class="pool-body">
          <div class="y-axis">
            <div v-for="day in days" :key="day" class="y-label">{{ day }}</div>
          </div>
          <div class="cells-and-x">
            <div class="cells">
              <div v-for="dayIdx in 7" :key="dayIdx" class="heatmap-row">
                <div
                  v-for="slotIdx in 48"
                  :key="slotIdx"
                  class="heatmap-cell"
                  :class="getCellClass(poolData, dayIdx - 1, slotIdx - 1)"
                  :style="getCellStyle(poolData, dayIdx - 1, slotIdx - 1)"
                  @mouseenter="showTooltip($event, poolData, dayIdx - 1, slotIdx - 1)"
                  @mouseleave="hideTooltip"
                ></div>
              </div>
            </div>
            <div class="x-axis">
              <div v-for="h in 24" :key="h" class="x-label">{{ String(h - 1).padStart(2, '0') }}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
    <div class="heatmap-legend">
      <span class="legend-label">Empty</span>
      <div class="legend-scale">
        <div
          v-for="level in 7"
          :key="level"
          class="legend-cell"
          :style="{ background: busyColor((level - 1) / 6) }"
        ></div>
      </div>
      <span class="legend-label">Full</span>
      <span class="legend-separator">|</span>
      <div class="legend-cell no-data"></div>
      <span class="legend-label">No data</span>
      <span class="legend-separator">|</span>
      <div class="legend-cell closed"></div>
      <span class="legend-label">Closed</span>
    </div>
    <div v-if="tooltip.visible" class="tooltip" :style="{ left: tooltip.x + 'px', top: tooltip.y + 'px' }">
      {{ tooltip.text }}
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import POOL_SCHEDULES from '../poolSchedules.json'

const props = defineProps({
  data: { type: Array, default: () => [] },
  knownPools: { type: Array, default: () => [] }
})

const tooltip = ref({ visible: false, x: 0, y: 0, text: '' })

const days = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

// Slots 0-47 represent 30-min intervals: slot 0 = 00:00, slot 14 = 07:00, …
// Each day entry is [firstOpenSlot, firstClosedSlot). Day indices: 0=Mon … 6=Sun.
// Schedules are defined in src/poolSchedules.json.

// Cache schedule look-ups per pool name so we only search once.
const scheduleCache = {}

function getSchedule(poolName) {
  if (poolName in scheduleCache) return scheduleCache[poolName]
  const lower = poolName.toLowerCase()
  for (const entry of POOL_SCHEDULES) {
    if (entry.match.some(k => lower.includes(k))) {
      scheduleCache[poolName] = entry.days
      return entry.days
    }
  }
  scheduleCache[poolName] = null
  return null
}

/**
 * Returns true when a slot should be treated as "closed".
 *
 * Priority:
 *  1. Official SWM schedule (definitive — overrides noisy utilisation data).
 *  2. Fallback for unknown pools: closed before 06:00 and from 23:00 onward.
 */
function isScheduleClosed(poolName, dayIdx, slotIdx) {
  const sched = getSchedule(poolName)
  if (sched) {
    const [open, close] = sched[dayIdx]
    return slotIdx < open || slotIdx >= close
  }
  // Unknown pool: no pool is open before 06:00 or from 23:00 onward.
  return slotIdx < 12 || slotIdx >= 46
}

// ---------------------------------------------------------------------------

const poolDataList = computed(() => {
  const byPool = {}

  // Seed empty grids for every pool known to the database so that pools with
  // no recent activity still appear in the heatmap instead of silently disappearing.
  for (const poolName of props.knownPools) {
    byPool[poolName] = Array(7).fill(null).map(() => Array(48).fill(null))
  }

  // Overlay actual heatmap data on top of the empty grids.
  for (const row of (props.data ?? [])) {
    if (!byPool[row.pool]) {
      byPool[row.pool] = Array(7).fill(null).map(() => Array(48).fill(null))
    }
    const d = (row.day_of_week + 6) % 7
    const s = row.slot
    byPool[row.pool][d][s] = { mean: row.mean, samples: row.samples, closedFraction: row.closed_fraction }
  }

  if (Object.keys(byPool).length === 0) return []

  return Object.entries(byPool)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([pool, grid]) => ({ pool, grid, breaks: computeBreaks(pool, grid) }))
})

// Compute 6 septile breakpoints from the valid open cells of a pool so that
// each of the 7 color bins contains an equal share of the actual data.
// Returns null if there are fewer than 7 valid cells (falls back to uniform).
function computeBreaks(poolName, grid) {
  const values = []
  for (let d = 0; d < 7; d++) {
    for (let s = 0; s < 48; s++) {
      if (isScheduleClosed(poolName, d, s)) continue
      const cell = grid[d][s]
      if (!cell || cell.samples < 5) continue
      if (cell.closedFraction >= 0.9) continue
      values.push(100 - cell.mean)
    }
  }
  if (values.length < 7) return null
  values.sort((a, b) => a - b)
  const breaks = []
  for (let i = 1; i <= 6; i++) {
    const pos = (i / 7) * (values.length - 1)
    const lo  = Math.floor(pos)
    const hi  = Math.ceil(pos)
    breaks.push(values[lo] + (pos - lo) * (values[hi] - values[lo]))
  }
  return breaks
}

// Maps t ∈ [0, 1] linearly from saturated blue to light blue.
//   t = 0 → #1937b4 (rgb 25,  55,  180) — empty pool
//   t = 1 → #dbeafe (rgb 219, 234, 254) — full / busy pool
// Light-blue top anchor keeps cell borders (#fff) visible against busy cells.
// No power curve needed: the per-pool septile binning already distributes the
// 7 discrete levels evenly across the actual data range.
function busyColor(t) {
  const r = Math.round(25  + t * 194)
  const g = Math.round(55  + t * 179)
  const b = Math.round(180 + t * 74)
  return `rgb(${r},${g},${b})`
}

function getCellClass(poolData, dayIdx, slotIdx) {
  if (isScheduleClosed(poolData.pool, dayIdx, slotIdx)) return 'closed'
  const cell = poolData.grid[dayIdx][slotIdx]
  if (!cell || cell.samples < 5) return 'no-data'
  if (cell.closedFraction >= 0.9) return 'closed'
  return null  // color applied via getCellStyle
}

// Returns the inline background style for busy (open, data-populated) cells.
// util = 100 − cell.mean converts stored remaining-capacity% to usage%:
//   util = 0  → empty  → dark blue
//   util = 100 → full  → white
// Level is determined by per-pool septile breakpoints so each of the 7 bins
// contains an equal share of the pool's data (falls back to uniform 14pp bins
// if the pool has fewer than 7 valid cells).
function getCellStyle(poolData, dayIdx, slotIdx) {
  if (isScheduleClosed(poolData.pool, dayIdx, slotIdx)) return null
  const cell = poolData.grid[dayIdx][slotIdx]
  if (!cell || cell.samples < 5) return null
  if (cell.closedFraction >= 0.9) return null
  const util = 100 - cell.mean
  let level
  if (poolData.breaks) {
    level = poolData.breaks.findIndex(bp => util <= bp)
    if (level === -1) level = 6
  } else {
    level = Math.min(6, Math.floor(util * 7 / 100))
  }
  return { background: busyColor(level / 6) }
}

function formatTime(slotIdx) {
  const hour = Math.floor(slotIdx / 2)
  const minute = (slotIdx % 2) * 30
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}

function showTooltip(event, poolData, dayIdx, slotIdx) {
  const x = event.clientX + 10
  const y = event.clientY + 10

  if (isScheduleClosed(poolData.pool, dayIdx, slotIdx)) {
    tooltip.value = { visible: true, x, y, text: 'Closed — outside opening hours' }
    return
  }
  const cell = poolData.grid[dayIdx][slotIdx]
  if (!cell || cell.samples < 5) {
    tooltip.value = { visible: true, x, y, text: 'Not enough data' }
    return
  }
  if (cell.closedFraction >= 0.9) {
    tooltip.value = { visible: true, x, y, text: 'Closed — 100% utilization' }
    return
  }
  const util = Math.round(100 - cell.mean)
  tooltip.value = { visible: true, x, y, text: `${util}% utilization (${cell.samples} samples)` }
}

function hideTooltip() {
  tooltip.value.visible = false
}
</script>

<style scoped>
.heatmap-container {
  background: white;
  border-radius: 8px;
  padding: 1rem;
  box-shadow: 0 1px 3px rgba(0,0,0,0.1);
}

.heatmap-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 1.25rem 2rem;
}

.heatmap-grid.single-pool {
  grid-template-columns: 1fr;
}

.pool-heatmap {
  min-width: 0;
}

.pool-header {
  font-size: 0.8rem;
  font-weight: 600;
  color: #333;
  margin-bottom: 4px;
}

.pool-body {
  display: flex;
  gap: 0;
  width: 100%;
}

.cells-and-x {
  flex: 1;
  min-width: 0;
}

.y-axis {
  display: flex;
  flex-direction: column;
  padding-right: 4px;
}

.y-label {
  height: 18px;
  font-size: 0.65rem;
  color: #666;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  white-space: nowrap;
}

.cells-and-x {
  display: flex;
  flex-direction: column;
}

.cells {
  display: flex;
  flex-direction: column;
}

.heatmap-row {
  display: flex;
  width: 100%;
}

.heatmap-cell {
  flex: 1 1 0;
  height: 18px;
  border: 1px solid #fff;
  cursor: pointer;
  min-width: 0;
}

.heatmap-cell.no-data { background: #e5e7eb; }
.heatmap-cell.closed  { background: #d1d5db; }

.x-axis {
  display: flex;
  margin-top: 2px;
}

.x-label {
  flex: 1 1 0;
  font-size: 0.6rem;
  color: #666;
  text-align: center;
  min-width: 0;
}

.heatmap-legend {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  margin-top: 1rem;
}

.legend-label {
  font-size: 0.7rem;
  color: #666;
}

.legend-scale {
  display: flex;
  gap: 1px;
}

.legend-cell {
  width: 12px;
  height: 12px;
  border-radius: 1px;
}

.legend-cell.no-data { background: #e5e7eb; }
.legend-cell.closed  { background: #d1d5db; }

.legend-separator {
  color: #ccc;
  font-size: 0.8rem;
  margin: 0 0.15rem;
}

@media (max-width: 900px) {
  .heatmap-grid {
    grid-template-columns: 1fr;
  }
}

.tooltip {
  position: fixed;
  background: #333;
  color: #fff;
  padding: 6px 10px;
  border-radius: 4px;
  font-size: 0.75rem;
  pointer-events: none;
  z-index: 1000;
  white-space: nowrap;
}
</style>
