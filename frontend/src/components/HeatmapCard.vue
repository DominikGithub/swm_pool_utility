<template>
  <div class="heatmap-container">
    <div class="heatmap-grid" :class="{ 'single-pool': poolDataList.length === 1 }">
      <div v-for="poolData in poolDataList" :key="poolData.pool" class="pool-heatmap">
        <div class="pool-header">
          <svg v-if="poolData.type === 'outdoor'" class="pool-type-icon outdoor" viewBox="0 0 16 16" role="img" aria-label="Outdoor pool (Freibad)">
            <title>Outdoor pool (Freibad)</title>
            <circle cx="8" cy="8" r="3" />
            <path d="M8 1v2M8 13v2M1 8h2M13 8h2M3.05 3.05l1.4 1.4M11.55 11.55l1.4 1.4M3.05 12.95l1.4-1.4M11.55 4.45l1.4-1.4" />
          </svg>
          <svg v-else-if="poolData.type === 'indoor'" class="pool-type-icon indoor" viewBox="0 0 16 16" role="img" aria-label="Indoor pool (Hallenbad)">
            <title>Indoor pool (Hallenbad)</title>
            <path d="M1.5 7 8 2l6.5 5" />
            <path d="M2.5 10.5q1.375-1.25 2.75 0t2.75 0 2.75 0 2.75 0M2.5 13.5q1.375-1.25 2.75 0t2.75 0 2.75 0 2.75 0" />
          </svg>{{ poolData.pool }}
        </div>
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
    <!-- "key", not "legend": style.css hides every div whose class contains "legend". -->
    <div class="heatmap-key">
      <span class="key-label">{{ scale ? `≤${Math.round(scale.lo)}%` : 'Low crowds' }}</span>
      <div class="key-scale">
        <div v-for="level in 6" :key="level" class="key-cell" :class="`level-${level - 1}`"></div>
      </div>
      <span class="key-label">{{ scale ? `≥${Math.round(scale.hi)}% full` : 'High crowds' }}</span>
      <span class="key-separator">|</span>
      <div class="key-cell closed"></div>
      <span class="key-label">Closed</span>
    </div>
    <p v-if="scale" class="key-note">Colours span this pool's own usual range, so quiet and busy times stand out even when it is never crowded.</p>
    <div v-if="tooltip.visible" class="tooltip" :style="{ left: tooltip.x + 'px', top: tooltip.y + 'px' }">
      {{ tooltip.text }}
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'

const props = defineProps({
  data: { type: Array, default: () => [] },
  knownPools: { type: Array, default: () => [] }
})

const tooltip = ref({ visible: false, x: 0, y: 0, text: '' })

const days = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

// ---------------------------------------------------------------------------
// Pool opening hours  (source: swm.de/baeder)
//
// Data is stored in src/data/pool-opening-hours.json so opening times can
// be updated independently of component logic.
//
// Time-slot system (applies to all pool types equally):
//
//   Each day is split into 48 fixed-length 30‑minute intervals:
//     slot 0  = 00:00–00:29      slot 14 = 07:00–07:29
//     slot 1  = 00:30–00:59      slot 15 = 07:30–07:59
//     …                          …
//     slot 47 = 23:30–23:59
//
//   There are 7 entries per schedule, one for each day of the week:
//     index 0 = Monday, 1 = Tuesday, … 6 = Sunday.
//
//   Each entry is [firstOpenSlot, firstClosedSlot) — a half-open interval.
//   The pool is considered open from firstOpenSlot (inclusive) up to but
//   NOT including firstClosedSlot.
//
//   Examples:
//     [14, 46]  →  open 07:00, close 23:00  (firstClosedSlot=46 → 23:00)
//     [20, 42]  →  open 10:00, close 21:00
//     [16, 36]  →  open 08:00, close 18:00
//
// Freibad (outdoor pool) closing times are weather‑dependent (set Mon/Thu
// by SWM based on the forecast).  The JSON uses 21:00 (slot 42) as an upper
// bound — the statistical closedFraction detection in the hourly-avg API
// catches actual earlier closings.
//
// Each entry also carries "type": "outdoor" (Freibad) or "indoor"
// (Hallenbad), shown as an icon next to the pool name.  The
// Dante-Winter-Warmfreibad is a heated open-air pool, so it counts as outdoor.
//
// Match ordering matters: more‑specific patterns (e.g. "dantebad") must
// appear before less‑specific ones (e.g. "dante") because getEntry()
// returns the first match.
// ---------------------------------------------------------------------------
import POOL_SCHEDULES from '@/data/pool-opening-hours.json'

// Cache entry look-ups per pool name so we only search once.
const entryCache = {}

function getEntry(poolName) {
  if (poolName in entryCache) return entryCache[poolName]
  const lower = poolName.toLowerCase()
  const entry = POOL_SCHEDULES.find(e => e.match.some(k => lower.includes(k))) ?? null
  entryCache[poolName] = entry
  return entry
}

function getSchedule(poolName) {
  return getEntry(poolName)?.days ?? null
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
    .map(([pool, grid]) => ({ pool, grid, type: getEntry(pool)?.type ?? null }))
})

// Returns the occupancy (% full) of an open cell with enough data, or the
// reason it has no colour: 'closed' or 'no-data'.
function cellState(poolData, dayIdx, slotIdx) {
  // 1. Official schedule: outside opening hours → closed
  if (isScheduleClosed(poolData.pool, dayIdx, slotIdx)) return 'closed'

  const cell = poolData.grid[dayIdx][slotIdx]
  if (!cell || cell.samples < 5) return 'no-data'

  // 2. Statistical detection: most samples at 100% → closed (holidays, etc.)
  if (cell.closedFraction >= 0.9) return 'closed'

  return 100 - cell.mean
}

// ---------------------------------------------------------------------------
// Colour scale
//
// All pools: fixed occupancy bands, so the same colour means the same crowd
// in every pool and the pools can be compared.
//
// One pool selected: the six colours span that pool's own usual range, from
// the 5th to the 95th percentile of its cells (the few most extreme cells
// would otherwise stretch the scale and flatten everything else). A pool
// that is never busy, like the Dante-Winter-Warmfreibad in autumn at 2–9%,
// would be a single shade of green on the fixed bands. The range is at least
// MIN_SPAN points wide so that a 1–2 point wobble in an almost empty pool
// does not flip colours.
// ---------------------------------------------------------------------------
const FIXED_BANDS = [12, 18, 25, 35, 60]
const MIN_SPAN = 12

const scale = computed(() => {
  if (poolDataList.value.length !== 1) return null
  const poolData = poolDataList.value[0]
  const occupancy = []
  for (let d = 0; d < 7; d++) {
    for (let s = 0; s < 48; s++) {
      const state = cellState(poolData, d, s)
      if (typeof state === 'number') occupancy.push(state)
    }
  }
  if (occupancy.length === 0) return null
  occupancy.sort((a, b) => a - b)
  const quantile = p => occupancy[Math.min(occupancy.length - 1, Math.floor(p * occupancy.length))]
  const lo = quantile(0.05)
  return { lo, hi: Math.max(quantile(0.95), lo + MIN_SPAN) }
})

function levelFor(occupancy) {
  const s = scale.value
  if (s) return Math.min(5, Math.max(0, Math.floor((occupancy - s.lo) / (s.hi - s.lo) * 6)))
  const band = FIXED_BANDS.findIndex(limit => occupancy < limit)
  return band === -1 ? 5 : band
}

function getCellClass(poolData, dayIdx, slotIdx) {
  const state = cellState(poolData, dayIdx, slotIdx)
  return typeof state === 'number' ? `level-${levelFor(state)}` : state
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

.pool-type-icon {
  width: 1em;
  height: 1em;
  margin-right: 0.3em;
  vertical-align: -0.15em;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.5;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.pool-type-icon.outdoor { color: #d97706; }
.pool-type-icon.indoor { color: #0066cc; }

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

.heatmap-cell.level-0 { background: #15803d; }
.heatmap-cell.level-1 { background: #22c55e; }
.heatmap-cell.level-2 { background: #86efac; }
.heatmap-cell.level-3 { background: #fde047; }
.heatmap-cell.level-4 { background: #fb923c; }
.heatmap-cell.level-5 { background: #ef4444; }
.heatmap-cell.no-data { background: #e5e7eb; }
.heatmap-cell.closed { background: #d1d5db; }

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

.heatmap-key {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-top: 1rem;
}

.key-label {
  font-size: 0.7rem;
  color: #666;
}

.key-scale {
  display: flex;
  gap: 2px;
}

.key-cell {
  width: 24px;
  height: 12px;
  border-radius: 2px;
}

.key-cell.level-0 { background: #15803d; }
.key-cell.level-1 { background: #22c55e; }
.key-cell.level-2 { background: #86efac; }
.key-cell.level-3 { background: #fde047; }
.key-cell.level-4 { background: #fb923c; }
.key-cell.level-5 { background: #ef4444; }
.key-cell.closed  { background: #d1d5db; }

.key-separator {
  color: #ccc;
  font-size: 0.8rem;
  margin: 0 0.15rem;
}

.key-note {
  margin-top: 0.35rem;
  font-size: 0.7rem;
  color: #888;
  text-align: center;
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
