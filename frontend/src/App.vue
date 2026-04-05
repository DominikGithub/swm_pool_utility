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
        <option value="weekday">Daily Average</option>
      </select>
      <button @click="fetchData">Refresh</button>
      <button v-show="!isWeekdayView" @click="toggleWeather" :class="{ active: showWeather }" class="weather-btn">
        <span class="weather-icon">{{ showWeather ? '🌤️' : '☁️' }}</span>
      </button>
    </div>

    <div v-if="loading" class="loading">Loading...</div>
    <template v-else>
      <div class="chart-container">
        <PoolChart :data="chartData" :weatherData="chartWeatherData" @hoverData="onHoverData" />
      </div>
      
      <div class="pool-list">
        <PoolCard 
          v-for="pool in currentPools" 
          :key="pool.name" 
          :pool="getPoolWithValue(pool)"
          :isFavorite="favorite === pool.name"
          @toggleFavorite="toggleFavorite(pool.name)"
        />
        <WeatherCard v-if="showWeather && !isWeekdayView" :weather="currentWeather" />
      </div>
      
    </template>
  </main>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import PoolChart from './components/PoolChart.vue'
import PoolCard from './components/PoolCard.vue'
import WeatherCard from './components/WeatherCard.vue'
import { fetchPools, fetchHistory, fetchWeather, fetchDailyAvg } from './composables/api'

const pools = ref([])
const historyData = ref([])
const dailyAvgData = ref({ labels: [], datasets: [] })
const weatherData = ref([])
const selectedPool = ref('')
const selectedDays = ref(1)
const loading = ref(true)
const favorite = ref('')
const hoverData = ref(null)
const hoverInfo = ref(null)
const showWeather = ref(localStorage.getItem('swm_showWeather') === 'true')

const isWeekdayView = computed(() => selectedDays.value === 'weekday')

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
  return date.toLocaleString('de-DE', {
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
  if (!historyData.value.length && !dailyAvgData.value.datasets.length) return []
  
  if (isWeekdayView.value) {
    return dailyAvgData.value.datasets.map(ds => ({
      name: ds.label,
      utility: null
    }))
  }

  const poolMap = new Map()
  const reversed = [...historyData.value].reverse()
  reversed.forEach(item => {
    if (!poolMap.has(item.name)) {
      const utilization = Math.max(0, 100 - item.utility)
      poolMap.set(item.name, { name: item.name, utility: utilization })
    }
  })
  return Array.from(poolMap.values()).slice(0, 12)
})

const CHART_COLORS = [
  '#0066cc', '#22c55e', '#eab308', '#ef4444', '#8b5cf6',
  '#ec4899', '#06b6d4', '#f97316', '#84cc16', '#64748b'
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
      const chartData = ds.data.map((v, idx) => v < 0 ? null : v)
      const stddev = ds.stddev || []

      if (isSinglePool) {
        datasets.push({
          label: ds.label + ' (lower)',
          data: chartData.map((v, idx) => v !== null ? Math.max(0, v - stddev[idx]) : null),
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
          data: chartData.map((v, idx) => v !== null ? Math.min(100, v + stddev[idx]) : null),
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
        data: chartData,
        borderColor: color,
        tension: 0.3,
        fill: false,
        spanGaps: true,
        borderDash: isSinglePool ? [] : [4, 4]
      })
    })

    return { labels: apiData.labels, datasets }
  }

  if (!historyData.value.length) return { labels: [], datasets: [] }

  const days = selectedDays.value
  let filtered = historyData.value
  
  if (days > 0) {
    const cutoff = new Date()
    cutoff.setDate(cutoff.getDate() - days)
    filtered = historyData.value.filter(d => new Date(d.timestamp) >= cutoff)
  }
  
  const labelSet = new Set()
  const poolGroups = {}
  filtered.forEach(item => {
    const label = formatTimestamp(item.timestamp)
    labelSet.add(label)
    if (!poolGroups[item.name]) poolGroups[item.name] = []
    const utilization = Math.max(0, 100 - item.utility)
    poolGroups[item.name].push({ x: label, y: utilization })
  })

  const labels = Array.from(labelSet).sort((a, b) => {
    const dateA = new Date(a.split(', ')[0].split('.').reverse().join('-') + 'T' + a.split(', ')[1])
    const dateB = new Date(b.split(', ')[0].split('.').reverse().join('-') + 'T' + b.split(', ')[1])
    return dateA - dateB
  })
  
  const datasets = Object.entries(poolGroups).map(([name, items], i) => ({
    label: name,
    data: items,
    borderColor: CHART_COLORS[i % CHART_COLORS.length],
    tension: 0.3,
    fill: false
  }))

  return { labels, datasets }
})

async function fetchData() {
  loading.value = true
  try {
    const isWeekday = selectedDays.value === 'weekday'

    if (isWeekday) {
      const data = await fetchDailyAvg(selectedPool.value)
      dailyAvgData.value = data
    } else {
      const fetchDays = selectedDays.value

      const params = new URLSearchParams()
      if (selectedPool.value) params.set('pool', selectedPool.value)
      params.set('days', fetchDays)
      
      const weatherParams = new URLSearchParams()
      weatherParams.set('days', fetchDays)
      
      const [history, weather] = await Promise.all([
        fetchHistory(params.toString()),
        fetchWeather(weatherParams.toString())
      ])
      
      historyData.value = history
      weatherData.value = weather
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
    dailyAvgData.value = await fetchDailyAvg(selectedPool.value)
  }
})
</script>
