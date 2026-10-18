<template>
  <header>
    <h1>SWM Pool Utilization</h1>
    <a
      href="https://github.com/DominikGithub/swm_pool_utility"
      target="_blank"
      rel="noopener noreferrer"
      class="github-link"
      aria-label="View SWM Pool Utilization on GitHub"
      title="View on GitHub"
    >
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
      </svg>
    </a>
  </header>
  <main>
    <div class="controls">
      <select v-model="selectedPool" @change="fetchData">
        <option value="">All Pools</option>
        <option v-for="pool in pools" :key="pool" :value="pool">{{ pool }}</option>
      </select>
      <select v-model="selectedDays" @change="onRangeChange">
        <option :value="1">{{ isLive ? 'Last 24 hours' : '1 day' }}</option>
        <option :value="3">{{ isLive ? 'Last 3 days' : '3 days' }}</option>
        <option :value="7">{{ isLive ? 'Last week' : '1 week' }}</option>
        <option :value="14">{{ isLive ? 'Last 2 weeks' : '2 weeks' }}</option>
        <option :value="30">{{ isLive ? 'Last month' : '1 month' }}</option>
        <option value="weekday">Daily Statistics</option>
        <option value="heatmap">Heatmap</option>
      </select>
      <button @click="refresh">Refresh</button>
      <button v-show="!isWeekdayView && !isHeatmapView" @click="toggleWeather" :class="{ active: showWeather }" class="weather-btn">
        <span class="weather-icon">{{ showWeather ? '🌤️' : '☁️' }}</span>
      </button>
    </div>

    <div v-if="loading" class="loading">Loading...</div>
    <template v-else>
      <div v-if="isHeatmapView" class="chart-container">
        <HeatmapCard
          :data="hourlyData"
          :knownPools="selectedPool ? [] : pools"
        />
      </div>
      <template v-else>
        <div class="chart-container" @mouseleave="onHoverData(null, null)">
          <div v-if="isWeekdayView" class="chart-key">
            <template v-if="selectedPool">
              <span class="key-item"><i class="swatch swatch-line"></i>Median week</span>
              <span class="key-item"><i class="swatch swatch-band"></i>Middle 50% of weeks (25th–75th percentile)</span>
            </template>
            <span v-else class="key-item">Lines: median week per pool</span>
            <span class="key-note">Days a pool was closed during opening hours are left out</span>
          </div>
          <div v-else-if="selectedPool && hasTypical" class="chart-key">
            <span class="key-item"><i class="swatch swatch-line"></i>Measured</span>
            <span v-if="hasForecast" class="key-item"><i class="swatch swatch-forecast"></i>Forecast</span>
            <span class="key-item"><i class="swatch swatch-typical"></i>Usual for this weekday</span>
          </div>
          <div class="chart-area" :class="{ refreshing }">
            <PoolChart :data="chartData" :weatherData="chartWeatherData" @hoverData="onHoverData" />
            <div v-if="!isWeekdayView && !(historyData ?? []).length" class="no-data-overlay">
              <template v-if="isLive">
                No data available for the selected pool and time range.
                <br>Try a longer time range, look further back below, or select a different pool.
              </template>
              <template v-else>No readings for the selected pool in this period.</template>
            </div>
          </div>
          <TimeNavigator
            v-if="!isWeekdayView && overview?.days?.length"
            :days="overview.days"
            :values="navigatorValues"
            :rangeDays="selectedDays"
            :windowEnd="windowEnd"
            @update:windowEnd="onWindowChange"
          />
        </div>
        
        <div class="pool-list">
          <PoolCard 
            v-for="pool in currentPools" 
            :key="pool.name" 
            :pool="getPoolWithValue(pool)"
            :isFavorite="favorite === pool.name"
            :status="isWeekdayView || !isLive ? null : poolStatuses[pool.name]"
            :reserveTrend="!isWeekdayView"
            @toggleFavorite="toggleFavorite(pool.name)"
          />
          <WeatherCard v-if="showWeather && !isWeekdayView" :weather="currentWeather" />
          <StatsCard v-if="isWeekdayView" :stats="dailyAvgStats" />
        </div>
      </template>
      
    </template>
  </main>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import PoolChart from './components/PoolChart.vue'
import PoolCard from './components/PoolCard.vue'
import WeatherCard from './components/WeatherCard.vue'
import StatsCard from './components/StatsCard.vue'
import HeatmapCard from './components/HeatmapCard.vue'
import TimeNavigator from './components/TimeNavigator.vue'
import {
  fetchPools, fetchHistory, fetchWeather, fetchDailyAvg, fetchDailyOverview, fetchHourlyAvg, fetchPoolStatus
} from './composables/api'

const pools = ref([])
const historyData = ref([])
const dailyAvgData = ref({ labels: [], datasets: [] })
const dailyAvgStats = ref(null)
// Daily statistics of the selected pool; their recent median is the live chart's
// "lately typical" line.
const typicalData = ref(null)
const hourlyData = ref([])
const weatherData = ref([])
const poolStatuses = ref({})
const selectedPool = ref('')
const selectedDays = ref(1)
const loading = ref(true)
const favorite = ref('')
const hoverData = ref(null)
const hoverInfo = ref(null)
const showWeather = ref(localStorage.getItem('swm_showWeather') === 'true')
// End of the chart's time window when looking into the past (ms of a Berlin day
// start, exclusive); null for the live window ending now.
const windowEnd = ref(null)
// Daily peaks over the whole history, for the time navigator.
const overview = ref(null)
// A quiet reload (moving the time window) dims the chart instead of replacing it.
const refreshing = ref(false)

const isWeekdayView = computed(() => selectedDays.value === 'weekday')
const isHeatmapView = computed(() => selectedDays.value === 'heatmap')
const isLive = computed(() => windowEnd.value == null)

// The selected pool's daily peaks, or for all pools the mean of their peaks.
const navigatorValues = computed(() => {
  const overviewPools = overview.value?.pools ?? []
  if (selectedPool.value) {
    return overviewPools.find(p => p.label === selectedPool.value)?.peak ?? overview.value.days.map(() => null)
  }
  return overview.value.days.map((_, i) => {
    const vals = overviewPools.map(p => p.peak[i]).filter(v => v != null)
    return vals.length ? vals.reduce((a, b) => a + b, 0) / vals.length : null
  })
})
const hasForecast = computed(() => chartData.value.datasets.some(ds => ds._prediction))
const hasTypical = computed(() =>
  chartData.value.datasets.some(ds => ds._typical && ds.data.some(p => p.y != null))
)

const emptyWeather = []

const chartWeatherData = computed(() => {
  if (isWeekdayView.value) return emptyWeather
  return showWeather.value ? weatherData.value : emptyWeather
})

const currentWeather = computed(() => {
  if (!showWeather.value) return null
  if (hoverInfo.value?.weather) return hoverInfo.value.weather
  if (weatherData.value.length > 0) return weatherData.value[weatherData.value.length - 1]
  return null
})

function onHoverData(values, info) {
  hoverData.value = values
  hoverInfo.value = info
}

function getCookie(name) {
  const match = document.cookie.match(new RegExp('(^| )' + name + '=([^;]+)'))
  return match ? decodeURIComponent(match[2]) : null
}

function setCookie(name, value, days = 365) {
  const expires = new Date(Date.now() + days * 864e5).toUTCString()
  document.cookie = name + '=' + encodeURIComponent(value) + '; expires=' + expires + '; path=/'
}

function toggleFavorite(poolName) {
  if (favorite.value === poolName) {
    favorite.value = ''
    setCookie('swm_favorite', '')
  } else {
    favorite.value = poolName
    setCookie('swm_favorite', poolName)
  }
  selectedPool.value = favorite.value
  fetchData()
}

function formatTimestamp(isoString) {
  const date = new Date(isoString)
  // Pin to Europe/Berlin so timestamps display in Munich local time (CET/CEST)
  // regardless of the viewer's browser timezone.
  return date.toLocaleString('de-DE', {
    timeZone: 'Europe/Berlin',
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

const berlinWeekTime = new Intl.DateTimeFormat('en-GB', {
  timeZone: 'Europe/Berlin',
  weekday: 'short',
  hour: '2-digit',
  minute: '2-digit',
  hourCycle: 'h23'
})
const WEEKDAY_INDEX = { Mon: 0, Tue: 1, Wed: 2, Thu: 3, Fri: 4, Sat: 5, Sun: 6 }

// Daily statistics slot for a timestamp: Monday=0 .. Sunday=6, 144 ten-minute
// slots per day, in Berlin wall-clock time like the daily-stats aggregator.
function berlinWeekSlot(isoString) {
  const parts = Object.fromEntries(
    berlinWeekTime.formatToParts(new Date(isoString)).map(p => [p.type, p.value])
  )
  return WEEKDAY_INDEX[parts.weekday] * 144 + Math.floor((Number(parts.hour) * 60 + Number(parts.minute)) / 10)
}

function getPoolWithValue(pool) {
  if (hoverData.value) {
    // A missing observation is unknown, not the pool's latest value. This
    // avoids showing a stale daytime value while hovering a historical gap.
    return { ...pool, utility: hoverData.value[pool.name] ?? null }
  }
  // In a past window the last reading would read like a live value.
  if (!isLive.value) return { ...pool, utility: null }
  return pool
}

const currentPools = computed(() => {
  const history = historyData.value ?? []
  const avgDatasets = dailyAvgData.value?.datasets ?? []

  if (!history.length && !avgDatasets.length) return []

  if (isWeekdayView.value) {
    return avgDatasets.map(ds => ({
      name: ds.label,
      utility: null
    }))
  }

  const poolMap = new Map()
  const reversed = [...history].reverse()
  reversed.forEach(item => {
    if (!poolMap.has(item.name)) {
      const utilization = Math.max(0, 100 - item.utility)
      poolMap.set(item.name, { name: item.name, utility: utilization })
    }
  })
  return Array.from(poolMap.values())
    .sort((a, b) => a.name.localeCompare(b.name))
})

const CHART_COLORS = [
  '#0066cc', '#22c55e', '#eab308', '#ef4444', '#8b5cf6',
  '#ec4899', '#06b6d4', '#f97316', '#84cc16', '#64748b',
  '#a78bfa', '#34d399', '#fb923c', '#f472b6', '#2dd4bf',
  '#c084fc', '#4ade80', '#fbbf24', '#e879f9', '#38bdf8'
]

function hexToRgba(hex, alpha) {
  const r = parseInt(hex.slice(1, 3), 16)
  const g = parseInt(hex.slice(3, 5), 16)
  const b = parseInt(hex.slice(5, 7), 16)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

const chartData = computed(() => {
  if (isWeekdayView.value) {
    if (!dailyAvgData.value.labels.length) return { labels: [], datasets: [] }

    const apiData = dailyAvgData.value
    const isSinglePool = selectedPool.value !== ''
    const datasets = []

    // Line = median week, band = P25–P75 (the middle half of observed weeks),
    // both computed by daily-stats with closure days excluded.
    const toPoints = values => apiData.labels.map((label, idx) => {
      const v = values?.[idx]
      return v == null || v < 0 ? { x: label, y: NaN } : { x: label, y: v }
    })

    apiData.datasets.forEach((ds, i) => {
      const color = CHART_COLORS[i % CHART_COLORS.length]

      if (isSinglePool) {
        datasets.push({
          label: ds.label + ' (P25)',
          data: toPoints(ds.p25),
          borderColor: 'transparent',
          borderWidth: 0,
          pointRadius: 0,
          pointHoverRadius: 0,
          pointHitRadius: 0,
          tension: 0.3,
          fill: false,
          spanGaps: true,
          _band: true
        })
        datasets.push({
          label: ds.label + ' (P75)',
          data: toPoints(ds.p75),
          borderColor: 'transparent',
          borderWidth: 0,
          pointRadius: 0,
          pointHoverRadius: 0,
          pointHitRadius: 0,
          tension: 0.3,
          backgroundColor: hexToRgba(color, 0.15),
          fill: '-1',
          spanGaps: true,
          _band: true
        })
      }

      datasets.push({
        label: ds.label,
        data: toPoints(ds.median),
        borderColor: color,
        tension: 0.3,
        fill: false,
        spanGaps: true,
        borderDash: isSinglePool ? [] : [4, 1]
      })
    })

    datasets.forEach(ds => { ds.data = ds.data.filter(p => p != null) })
    return { labels: apiData.labels, datasets, historyLength: apiData.labels.length }
  }

  const history = historyData.value ?? []
  if (!history.length) return { labels: [], datasets: [], timestamps: [] }

  const days = selectedDays.value
  let filtered = history
  
  // A past window is bounded by the API already.
  if (days > 0 && isLive.value) {
    const cutoff = new Date()
    cutoff.setDate(cutoff.getDate() - days)
    filtered = historyData.value.filter(d => new Date(d.timestamp) >= cutoff)
  }
  
  // Build a list of unique (label, isoTimestamp) pairs, ordered by time.
  // The API returns Berlin-local RFC3339 (e.g. "2026-04-06T10:30:00+02:00"),
  // so Date objects constructed from them are UTC-correct for comparisons.
  const labelMap = new Map() // label -> ISO timestamp
  const poolGroups = {}
  filtered.forEach(item => {
    const label = formatTimestamp(item.timestamp)
    if (!labelMap.has(label)) {
      labelMap.set(label, item.timestamp)
    }
    if (!poolGroups[item.name]) poolGroups[item.name] = []
    const utilization = Math.max(0, 100 - item.utility)
    poolGroups[item.name].push({ x: label, y: utilization })
  })

  // Sort by actual date value (reliable, no string-parsing heuristics).
  const sortedEntries = Array.from(labelMap.entries()).sort((a, b) => {
    return new Date(a[1]) - new Date(b[1])
  })
  const labels = sortedEntries.map(e => e[0])
  const timestamps = sortedEntries.map(e => e[1])

  const datasets = Object.entries(poolGroups).map(([name, items], i) => {
    // Break the line when consecutive data points are more than ~30 min apart
    // (3 label slots at the 10-min scrape interval).  This prevents chart.js
    // from drawing a diagonal line across multi-day gaps (e.g. Michaelibad
    // closed for renovations Apr 27 – May 22, 2026).
    const data = []
    const sorted = [...items].sort((a, b) => {
      const ai = labels.indexOf(a.x); const bi = labels.indexOf(b.x)
      return ai - bi
    })
    for (let j = 0; j < sorted.length; j++) {
      data.push(sorted[j])
      if (j < sorted.length - 1) {
        const ai = labels.indexOf(sorted[j].x)
        const bi = labels.indexOf(sorted[j + 1].x)
        if (bi - ai > 3) {
          data.push({ x: labels[Math.floor((ai + bi) / 2)], y: null })
        }
      }
    }
    return {
      label: name,
      data,
      borderColor: CHART_COLORS[i % CHART_COLORS.length],
      spanGaps: false,
      tension: 0.3,
      fill: false
    }
  })

  // Number of historical labels before prediction labels are appended.
  // Used by the chart to clamp the crosshair to the last measured point.
  const historyLength = labels.length

  // Append prediction lines (dashed) for each pool — live views up to 3 days only.
  // Uses pred_series (all N steps) when available, falling back to the two-point
  // pred_1h / pred_2h for backwards compatibility with old DB rows.
  const preds = poolStatuses.value
  if (isLive.value && labels.length > 0 && timestamps.length > 0 && Object.keys(preds).length > 0 && days <= 3) {
    const now = new Date()

    // Derive the step interval from the series length of the first pool that has one.
    // Series covers a fixed 2-hour horizon, so interval = 120 min / N steps.
    const firstSeries = Object.values(preds).map(s => s.pred_series).find(s => Array.isArray(s) && s.length > 0)
    const horizonMinutes = 120
    const seriesLen = firstSeries ? firstSeries.length : 0

    if (seriesLen > 0) {
      const intervalMinutes = horizonMinutes / seriesLen

      // Build shared future timestamps for all pools (same N steps).
      const predEntries = Array.from({ length: seriesLen }, (_, i) => {
        const t = new Date(now.getTime() + (i + 1) * intervalMinutes * 60 * 1000)
        return { label: formatTimestamp(t.toISOString()), iso: t.toISOString() }
      })
      predEntries.forEach(({ label, iso }) => {
        labels.push(label)
        timestamps.push(iso)
      })

      Object.entries(poolGroups).forEach(([name, items], i) => {
        const status = preds[name]
        if (!status?.pred_series?.length) return
        const baseColor = CHART_COLORS[i % CHART_COLORS.length]
        datasets.push({
          label: name + ' (predicted)',
          data: [
            items[items.length - 1],
            ...status.pred_series.map((val, j) => ({ x: predEntries[j].label, y: val }))
          ],
          borderColor: baseColor,
          borderDash: [5, 5],
          borderWidth: 2,
          tension: 0.3,
          fill: false,
          pointRadius: 0,
          _prediction: true
        })
      })
    } else {
      // Fallback: old rows without pred_series — draw two endpoints only.
      const pred1h = new Date(now.getTime() + 60 * 60 * 1000)
      const pred2h = new Date(now.getTime() + 2 * 60 * 60 * 1000)
      const label1h = formatTimestamp(pred1h.toISOString())
      const label2h = formatTimestamp(pred2h.toISOString())
      labels.push(label1h, label2h)
      timestamps.push(pred1h.toISOString(), pred2h.toISOString())
      Object.entries(poolGroups).forEach(([name, items], i) => {
        const status = preds[name]
        if (!status) return
        const baseColor = CHART_COLORS[i % CHART_COLORS.length]
        datasets.push({
          label: name + ' (predicted)',
          data: [
            items[items.length - 1],
            { x: label1h, y: status.pred_1h },
            { x: label2h, y: status.pred_2h }
          ],
          borderColor: baseColor,
          borderDash: [5, 5],
          borderWidth: 2,
          tension: 0.3,
          fill: false,
          pointRadius: 0,
          _prediction: true
        })
      })
    }
  }

  // Single pool: what has lately been typical for each weekday and time (median
  // of the 28 days before today, see daily-stats), also across the forecast.  Skipped
  // for a pool closed the whole window (an outdoor pool after the season):
  // its recent median is 0 at night and missing during opening hours.  Live
  // only: next to a past window, today's "lately" would be the wrong yardstick.
  const typical = typicalData.value?.datasets?.find(ds => ds.label === selectedPool.value)
  if (isLive.value && typical?.recent_median?.some(v => v > 0)) {
    datasets.push({
      label: 'Lately typical',
      data: timestamps.map((ts, k) => {
        const v = typical.recent_median[berlinWeekSlot(ts)]
        return { x: labels[k], y: v >= 0 ? v : null }
      }),
      borderColor: '#64748b',
      borderDash: [2, 3],
      borderWidth: 1.5,
      pointRadius: 0,
      tension: 0.3,
      fill: false,
      spanGaps: false,
      _typical: true
    })
  }

  datasets.forEach(ds => { ds.data = ds.data.filter(p => p != null) })
  return { labels, datasets, timestamps, historyLength }
})

// days (and end, for a past window) for the history and weather endpoints.
function windowParams(days) {
  const params = new URLSearchParams()
  params.set('days', days)
  if (windowEnd.value != null) params.set('end', new Date(windowEnd.value).toISOString())
  return params
}

let fetchSeq = 0

// quiet: keep the chart on screen (dimmed) while loading, for moving the time
// window.  Only the latest request's response is applied.
async function fetchData({ quiet = false } = {}) {
  const seq = ++fetchSeq
  if (quiet) refreshing.value = true
  else loading.value = true
  try {
    const isWeekday = selectedDays.value === 'weekday'
    const isHeatmap = selectedDays.value === 'heatmap'

    if (isHeatmap) {
      const data = await fetchHourlyAvg(selectedPool.value)
      if (seq !== fetchSeq) return
      hourlyData.value = data
    } else if (isWeekday) {
      const data = await fetchDailyAvg(selectedPool.value)
      if (seq !== fetchSeq) return
      dailyAvgData.value = data
      dailyAvgStats.value = {
        weeks: data.weeks,
        total_samples: data.total_samples,
        date_from: data.date_from,
        date_to: data.date_to,
        updated_at: data.updated_at
      }
    } else {
      const params = windowParams(selectedDays.value)
      if (selectedPool.value) params.set('pool', selectedPool.value)

      const [history, weather, statuses, typical] = await Promise.all([
        fetchHistory(params.toString()),
        fetchWeather(windowParams(selectedDays.value).toString()),
        fetchPoolStatus(),
        selectedPool.value ? fetchDailyAvg(selectedPool.value) : null
      ])
      if (seq !== fetchSeq) return

      typicalData.value = typical
      historyData.value = history ?? []
      weatherData.value = weather ?? []
      poolStatuses.value = Object.fromEntries((statuses ?? []).map(s => [s.name, s]))
    }
  } catch (err) {
    console.error('Failed to fetch data:', err)
  } finally {
    if (seq === fetchSeq) {
      loading.value = false
      refreshing.value = false
    }
  }
}

async function loadOverview() {
  try {
    overview.value = await fetchDailyOverview()
  } catch (err) {
    console.error('Failed to fetch daily overview:', err)
  }
}

function refresh() {
  loadOverview()
  fetchData()
}

function onWindowChange(end) {
  windowEnd.value = end
  fetchData({ quiet: true })
}

// A longer range must still fit after the first recorded day.
function onRangeChange() {
  const days = overview.value?.days ?? []
  const idx = days.findIndex(d => Date.parse(d) === windowEnd.value)
  if (typeof selectedDays.value === 'number' && idx >= 0 && idx < selectedDays.value) {
    windowEnd.value = Date.parse(days[Math.min(selectedDays.value, days.length - 1)])
  }
  fetchData()
}

async function toggleWeather() {
  showWeather.value = !showWeather.value
  localStorage.setItem('swm_showWeather', showWeather.value)
  if (showWeather.value && weatherData.value.length === 0) {
    try {
      weatherData.value = await fetchWeather(windowParams(selectedDays.value).toString())
    } catch (err) {
      console.error('Failed to fetch weather:', err)
    }
  }
}

onMounted(async () => {
  loadOverview()
  pools.value = await fetchPools()
  favorite.value = getCookie('swm_favorite') || ''
  selectedPool.value = favorite.value
  await fetchData()
  if (showWeather.value && weatherData.value.length === 0) {
    try {
      weatherData.value = await fetchWeather(windowParams(selectedDays.value).toString())
    } catch (err) {
      console.error('Failed to fetch weather:', err)
    }
  }
  if (selectedDays.value === 'weekday') {
    const data = await fetchDailyAvg(selectedPool.value)
    dailyAvgData.value = data
    dailyAvgStats.value = {
      weeks: data.weeks,
      total_samples: data.total_samples,
      date_from: data.date_from,
      date_to: data.date_to,
      updated_at: data.updated_at
    }
  }
})
</script>

<style scoped>
.chart-container {
  position: relative;
}

.chart-area {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  position: relative;
  transition: opacity 0.15s ease;
}

.chart-area.refreshing {
  opacity: 0.55;
}

.chart-key {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 1rem;
  padding: 0.6rem 0.9rem 0;
  font-size: 0.75rem;
  color: #4b5563;
}

.key-item {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
}

.key-note {
  color: #9ca3af;
}

.swatch {
  display: inline-block;
  width: 25px;
  height: 10px;
  border-radius: 2px;
}

/* Single-pool view always draws with CHART_COLORS[0]. */
.swatch-line {
  height: 3px;
  background: #0066cc;
}

/* Dash patterns match the chart: forecast borderDash [5, 5], typical [2, 3]. */
.swatch-forecast {
  height: 2px;
  border-radius: 0;
  background: repeating-linear-gradient(to right, #0066cc 0 5px, transparent 5px 10px);
}

.swatch-typical {
  height: 2px;
  border-radius: 0;
  background: repeating-linear-gradient(to right, #64748b 0 2px, transparent 2px 5px);
}

.swatch-band {
  background: rgba(0, 102, 204, 0.15);
}

.no-data-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: #6b7280;
  font-size: 0.9rem;
  text-align: center;
  pointer-events: none;
  line-height: 1.6;
}
</style>
