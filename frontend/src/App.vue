<template>
  <header>
    <h1>SWM Pool Utilization</h1>
  </header>
  <main>
    <div class="controls">
      <select v-model="selectedPool" @change="fetchData">
        <option value="">All Pools</option>
        <option v-for="pool in pools" :key="pool" :value="pool">{{ pool }}</option>
      </select>
      <select v-model="selectedDays" @change="fetchData">
        <option :value="1">Last 24 hours</option>
        <option :value="3">Last 3 days</option>
        <option :value="7">Last week</option>
        <option :value="14">Last 2 weeks</option>
        <option :value="30">Last month</option>
        <option value="weekday">Daily Statistics</option>
        <option value="heatmap">Heatmap</option>
      </select>
      <button @click="fetchData">Refresh</button>
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
          <PoolChart :data="chartData" :weatherData="chartWeatherData" @hoverData="onHoverData" />
          <div v-if="!isWeekdayView && !(historyData ?? []).length" class="no-data-overlay">
            No data available for the selected pool and time range.
            <br>Try a longer time range or select a different pool.
          </div>
        </div>
        
        <div class="pool-list">
          <PoolCard 
            v-for="pool in currentPools" 
            :key="pool.name" 
            :pool="getPoolWithValue(pool)"
            :isFavorite="favorite === pool.name"
            :status="isWeekdayView ? null : poolStatuses[pool.name]"
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
import { fetchPools, fetchHistory, fetchWeather, fetchDailyAvg, fetchHourlyAvg, fetchPoolStatus } from './composables/api'

const pools = ref([])
const historyData = ref([])
const dailyAvgData = ref({ labels: [], datasets: [] })
const dailyAvgStats = ref(null)
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

const isWeekdayView = computed(() => selectedDays.value === 'weekday')
const isHeatmapView = computed(() => selectedDays.value === 'heatmap')

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

function getPoolWithValue(pool) {
  if (hoverData.value && hoverData.value[pool.name] !== undefined) {
    return { ...pool, utility: hoverData.value[pool.name] }
  }
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

    apiData.datasets.forEach((ds, i) => {
      const color = CHART_COLORS[i % CHART_COLORS.length]
      const rawData = ds.data
      const stddev = ds.stddev || []

      const points = apiData.labels.map((label, idx) => {
        const v = rawData[idx]
        return v < 0 ? null : { x: label, y: v }
      })

      if (isSinglePool) {
        datasets.push({
          label: ds.label + ' (lower)',
          data: points.map((p, idx) => p !== null ? { x: p.x, y: Math.max(0, p.y - (stddev[idx] || 0)) } : null),
          borderColor: 'transparent',
          borderWidth: 0,
          pointRadius: 0,
          pointHoverRadius: 0,
          pointHitRadius: 0,
          tension: 0.3,
          fill: false,
          spanGaps: true,
          _ci: true
        })
        datasets.push({
          label: ds.label + ' (upper)',
          data: points.map((p, idx) => p !== null ? { x: p.x, y: Math.min(100, p.y + (stddev[idx] || 0)) } : null),
          borderColor: 'transparent',
          borderWidth: 0,
          pointRadius: 0,
          pointHoverRadius: 0,
          pointHitRadius: 0,
          tension: 0.3,
          backgroundColor: hexToRgba(color, 0.15),
          fill: '-1',
          spanGaps: true,
          _ci: true
        })
      }

      datasets.push({
        label: ds.label,
        data: points,
        borderColor: color,
        tension: 0.3,
        fill: false,
        spanGaps: true,
        borderDash: isSinglePool ? [] : [4, 1]
      })
    })

    return { labels: apiData.labels, datasets, historyLength: apiData.labels.length }
  }

  const history = historyData.value ?? []
  if (!history.length) return { labels: [], datasets: [], timestamps: [] }

  const days = selectedDays.value
  let filtered = history
  
  if (days > 0) {
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

  const datasets = Object.entries(poolGroups).map(([name, items], i) => ({
    label: name,
    data: items,
    borderColor: CHART_COLORS[i % CHART_COLORS.length],
    tension: 0.3,
    fill: false
  }))

  // Number of historical labels before prediction labels are appended.
  // Used by the chart to clamp the crosshair to the last measured point.
  const historyLength = labels.length

  // Append prediction lines (dashed) for each pool — shown for views up to 3 days.
  // Uses pred_series (all N steps) when available, falling back to the two-point
  // pred_1h / pred_2h for backwards compatibility with old DB rows.
  const preds = poolStatuses.value
  if (labels.length > 0 && timestamps.length > 0 && Object.keys(preds).length > 0 && days <= 3) {
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

  return { labels, datasets, timestamps, historyLength }
})

async function fetchData() {
  loading.value = true
  try {
    const isWeekday = selectedDays.value === 'weekday'
    const isHeatmap = selectedDays.value === 'heatmap'

    if (isHeatmap) {
      hourlyData.value = await fetchHourlyAvg(selectedPool.value)
    } else if (isWeekday) {
      const data = await fetchDailyAvg(selectedPool.value)
      dailyAvgData.value = data
      dailyAvgStats.value = {
        weeks: data.weeks,
        total_samples: data.total_samples,
        date_from: data.date_from,
        date_to: data.date_to,
        updated_at: data.updated_at
      }
    } else {
      const fetchDays = selectedDays.value

      const params = new URLSearchParams()
      if (selectedPool.value) params.set('pool', selectedPool.value)
      params.set('days', fetchDays)
      
      const weatherParams = new URLSearchParams()
      weatherParams.set('days', fetchDays)
      
      const [history, weather, statuses] = await Promise.all([
        fetchHistory(params.toString()),
        fetchWeather(weatherParams.toString()),
        fetchPoolStatus()
      ])
      
      historyData.value = history ?? []
      weatherData.value = weather ?? []
      poolStatuses.value = Object.fromEntries((statuses ?? []).map(s => [s.name, s]))
    }
  } catch (err) {
    console.error('Failed to fetch data:', err)
  }
  loading.value = false
}

async function toggleWeather() {
  showWeather.value = !showWeather.value
  localStorage.setItem('swm_showWeather', showWeather.value)
  if (showWeather.value && weatherData.value.length === 0) {
    try {
      const weatherParams = new URLSearchParams()
      weatherParams.set('days', selectedDays.value)
      weatherData.value = await fetchWeather(weatherParams.toString())
    } catch (err) {
      console.error('Failed to fetch weather:', err)
    }
  }
}

onMounted(async () => {
  pools.value = await fetchPools()
  favorite.value = getCookie('swm_favorite') || ''
  selectedPool.value = favorite.value
  await fetchData()
  if (showWeather.value && weatherData.value.length === 0) {
    try {
      const weatherParams = new URLSearchParams()
      weatherParams.set('days', selectedDays.value)
      weatherData.value = await fetchWeather(weatherParams.toString())
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
