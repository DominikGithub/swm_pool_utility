<template>
  <div class="heatmap-container">
    <div
      ref="gridEl"
      class="heatmap-grid"
      :class="{ 'single-pool': isSinglePool }"
      @mousemove="onGridPointer"
      @click="onGridPointer"
      @mouseleave="onGridLeave"
    >
      <div v-for="poolData in poolDataList" :key="poolData.pool" class="pool-heatmap" :data-pool="poolData.pool">
        <!-- While the pointer is over the other pool of this row, this slot shows that pool's histogram. -->
        <template v-if="histogramIn(poolData.pool)">
          <div class="pool-header hist-header">
            <span class="hist-arrow">{{ inspected.arrows[poolData.pool] }}</span>{{ inspected.pool }}<span class="hist-sub"> · {{ statsLine(inspected.pool) }}</span>
          </div>
          <PoolHistogram v-bind="histogramProps(inspected.pool)" />
        </template>
        <template v-else>
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
        <!-- Per-pool key: collapsed until the pointer is over this row of pools. -->
        <div v-if="perPool" class="pool-key-wrap" :class="{ open: isSinglePool || openPools.has(poolData.pool) }">
          <div class="pool-key-inner">
            <div v-if="poolScales[poolData.pool]" class="heatmap-key pool-key">
              <span class="key-label">≤{{ fmt(poolScales[poolData.pool].lo) }}%</span>
              <div class="key-scale ticked">
                <div v-for="level in 6" :key="level" class="key-cell" :class="`level-${level - 1}`"></div>
                <span
                  v-for="(tick, i) in ticks(poolScales[poolData.pool])"
                  :key="i"
                  class="key-tick"
                  :style="{ left: `${(i * 100) / 6}%` }"
                >{{ tick }}</span>
              </div>
              <span class="key-label">≥{{ fmt(poolScales[poolData.pool].hi) }}% full</span>
              <!-- Single pool: the global key below is hidden, so the closed swatch moves here. -->
              <template v-if="isSinglePool">
                <span class="key-separator">|</span>
                <div class="key-cell closed"></div>
                <span class="key-label">Closed</span>
              </template>
            </div>
            <p v-else class="key-note">No open readings in this period</p>
          </div>
        </div>
        </template>
      </div>
      <!-- Spare slot: next to the last pool of an odd row (wide screens), showing its
           histogram on hover, and below a single selected pool, where it always shows. -->
      <div v-if="hasSpare" class="pool-heatmap spare" :data-pool="SPARE">
        <template v-if="sparePool">
          <div class="pool-header hist-header">
            <span class="hist-arrow">{{ isSinglePool ? '▲' : '◀' }}</span>{{ sparePool }}<span class="hist-sub"> · {{ statsLine(sparePool) }}</span>
          </div>
          <PoolHistogram v-bind="histogramProps(sparePool)" />
        </template>
      </div>
    </div>
    <!-- "key", not "legend": style.css hides every div whose class contains "legend".
         Hidden for a single pool in per-pool mode, where that pool's own key is shown. -->
    <div v-if="!(perPool && isSinglePool)" class="heatmap-key">
      <span class="key-label">{{ perPool ? 'Quiet for the pool' : 'Low crowds' }}</span>
      <div class="key-scale">
        <div v-for="level in 6" :key="level" class="key-cell" :class="`level-${level - 1}`"></div>
      </div>
      <span class="key-label">{{ perPool ? 'Busy for the pool' : 'High crowds' }}</span>
      <span class="key-separator">|</span>
      <div class="key-cell closed"></div>
      <span class="key-label">Closed</span>
    </div>
    <div v-if="tooltip.visible" class="tooltip" :style="{ left: tooltip.x + 'px', top: tooltip.y + 'px' }">
      {{ tooltip.text }}
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import PoolHistogram from './PoolHistogram.vue'

const props = defineProps({
  data: { type: Array, default: () => [] },
  knownPools: { type: Array, default: () => [] },
  // 'fixed': same occupancy bands for every pool; 'pool': each pool's own range.
  scaleMode: { type: String, default: 'fixed' }
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
// by SWM based on the forecast) and not published as fixed times.  The JSON
// uses 20:00 (slot 40) as an upper bound: in the whole 2026 season no
// Freibad had open readings after 20:15 — the statistical closedFraction
// detection in the hourly-avg API catches earlier closings.  Exception:
// the Dantebad's stadium area is open 7:00/7:30–23:00 in summer too
// (swm.de, checked 11/2026 incl. the archived summer 2026 pages).
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
// Colour scale (scaleMode prop, toggled above the heatmap)
//
// 'fixed': the same occupancy bands for every pool, so a colour means the
// same crowd everywhere and the pools can be compared.
//
// 'pool': the six colours span each pool's own usual range. The range runs
// from the 5th to the 95th percentile of the pool's open half-hour cells, so
// the quietest 5% of its week (typically the first and last half hour of a
// day) and the busiest 5% (typically weekend afternoons) are clipped to the
// end colours instead of stretching the scale and flattening everything
// else. A pool that is never busy, like the Dante-Winter-Warmfreibad in
// autumn at 2–9%, is a single shade of green on the fixed bands but shows
// its daily pattern here.
// ---------------------------------------------------------------------------
const FIXED_BANDS = [12, 18, 25, 35, 60]
const CLIP = 0.05

const perPool = computed(() => props.scaleMode === 'pool')
const isSinglePool = computed(() => poolDataList.value.length === 1)

// Occupancy of a pool's open cells (sorted) and its percentiles: the input of
// both the per-pool colour range and the histogram.
function cellStats(poolData) {
  const values = []
  for (let d = 0; d < 7; d++) {
    for (let s = 0; s < 48; s++) {
      const state = cellState(poolData, d, s)
      if (typeof state === 'number') values.push(state)
    }
  }
  if (values.length === 0) return { values, n: 0 }
  values.sort((a, b) => a - b)
  const quantile = p => values[Math.min(values.length - 1, Math.floor(p * values.length))]
  const lo = quantile(CLIP)
  const hi = quantile(1 - CLIP)
  // all cells equal: avoid a zero-width range
  return { values, n: values.length, median: quantile(0.5), lo, hi: hi > lo ? hi : lo + 1 }
}

const poolStats = computed(() => Object.fromEntries(poolDataList.value.map(p => [p.pool, cellStats(p)])))

// pool name → { lo, hi } in % full, or null when the pool has no open cells.
const poolScales = computed(() => {
  if (!perPool.value) return {}
  return Object.fromEntries(Object.entries(poolStats.value).map(([pool, s]) => [pool, s.n ? { lo: s.lo, hi: s.hi } : null]))
})

function levelFor(occupancy, pool) {
  const range = perPool.value ? poolScales.value[pool] : null
  if (range) return Math.min(5, Math.max(0, Math.floor(((occupancy - range.lo) / (range.hi - range.lo)) * 6)))
  const band = FIXED_BANDS.findIndex(limit => occupancy < limit)
  return band === -1 ? 5 : band
}

function getCellClass(poolData, dayIdx, slotIdx) {
  const state = cellState(poolData, dayIdx, slotIdx)
  return typeof state === 'number' ? `level-${levelFor(state, poolData.pool)}` : state
}

const fmt = v => String(Math.round(v * 10) / 10)

// The seven colour boundaries of a per-pool range, for the tick labels.
function ticks(range) {
  return Array.from({ length: 7 }, (_, i) => fmt(range.lo + (i * (range.hi - range.lo)) / 6))
}

// ---------------------------------------------------------------------------
// Pointer over the grid (rows are found by height, so the gap between the
// two pools of a row keeps the row):
//
// - Per-pool keys open for the row of pools under the pointer (two pools per
//   row on wide screens, one on narrow ones).
// - The pool slot under the pointer is "inspected": its row partner shows the
//   inspected pool's histogram instead of its own heatmap, so moving across a
//   row flips the two sides. A pool alone in the last row of an odd count uses
//   the spare slot next to it. One pool per row (narrow screens): no partner.
//
// Both reset when the pointer leaves the grid or is between two rows.
// ---------------------------------------------------------------------------
const SPARE = '__spare__'
const gridEl = ref(null)
const openRow = ref('') // names of the pools in the open row, joined by '\n'
const openPools = computed(() => new Set(openRow.value ? openRow.value.split('\n') : []))
const inspected = ref(null) // { pool, arrows: { slot name → '◀' | '▶' toward the pool } }

const hasSpare = computed(() => isSinglePool.value || poolDataList.value.length % 2 === 1)
// The spare slot always shows the single selected pool; otherwise only on hover.
const sparePool = computed(() => (isSinglePool.value ? poolDataList.value[0]?.pool : histogramIn(SPARE) ? inspected.value.pool : null))

function histogramIn(slot) {
  return Boolean(inspected.value && slot in inspected.value.arrows)
}

function histogramProps(pool) {
  const s = poolStats.value[pool]
  return { values: s.values, stats: s.n ? s : null, clipped: perPool.value, level: v => levelFor(v, pool), height: HISTOGRAM_HEIGHT }
}

function statsLine(pool) {
  const s = poolStats.value[pool]
  if (!s.n) return 'no open readings'
  const clip = perPool.value ? ` · p5 ${fmt(s.lo)}% · p95 ${fmt(s.hi)}%` : ''
  return `${s.n} half-hour slots · median ${fmt(s.median)}%${clip}`
}

// Height of a heatmap body — 7 rows of 18px + 2px gap + 11px hour labels (fixed
// line-height in .x-label) — so a histogram can replace it without moving the layout.
const HISTOGRAM_HEIGHT = 7 * 18 + 2 + 11

function onGridPointer(event) {
  if (isSinglePool.value || !gridEl.value) return
  const items = [...gridEl.value.querySelectorAll('.pool-heatmap')].filter(el => el.offsetParent !== null)
  const inRow = items.find(el => {
    const r = el.getBoundingClientRect()
    return event.clientY >= r.top && event.clientY <= r.bottom
  })
  const rowItems = inRow ? items.filter(el => el.offsetTop === inRow.offsetTop) : []

  const row = perPool.value ? rowItems.map(el => el.dataset.pool).filter(p => p !== SPARE).join('\n') : ''
  if (row !== openRow.value) openRow.value = row

  if (!inRow) {
    inspected.value = null
    return
  }
  const self = rowItems.find(el => {
    const r = el.getBoundingClientRect()
    return event.clientX >= r.left && event.clientX <= r.right
  })
  if (!self || self.dataset.pool === SPARE || self.dataset.pool === inspected.value?.pool) return // gap or histogram: keep
  const mates = rowItems.filter(el => el !== self)
  inspected.value = {
    pool: self.dataset.pool,
    arrows: Object.fromEntries(mates.map(el => [el.dataset.pool, el.offsetLeft > self.offsetLeft ? '◀' : '▶'])),
  }
}

function onGridLeave() {
  openRow.value = ''
  inspected.value = null
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
  line-height: 11px; /* fixed, so HISTOGRAM_HEIGHT matches the heatmap body exactly */
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

/* Per-pool key: a 0fr → 1fr grid row animates the height without a fixed max-height. */
.pool-key-wrap {
  display: grid;
  grid-template-rows: 0fr;
  transition: grid-template-rows 0.2s ease;
}

.pool-key-wrap.open {
  grid-template-rows: 1fr;
}

.pool-key-inner {
  overflow: hidden;
  min-height: 0;
}

.heatmap-key.pool-key {
  margin-top: 0.5rem;
  padding-bottom: 0.9rem; /* room for the tick labels */
}

.key-scale.ticked {
  position: relative;
}

.key-tick {
  position: absolute;
  top: 100%;
  margin-top: 2px;
  transform: translateX(-50%);
  font-size: 0.6rem;
  color: #888;
  white-space: nowrap;
}

/* Histogram standing in for a heatmap: one-line header, same height as a pool header. */
.hist-header {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.hist-arrow {
  color: #0066cc;
  margin-right: 0.35em;
}

.hist-sub {
  font-weight: 400;
  color: #666;
}

/* One pool per row: no partner to swap with, so the spare slot is only for a single selected pool. */
@media (max-width: 900px) {
  .heatmap-grid:not(.single-pool) .spare {
    display: none;
  }
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
