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
import { fetchPools, fetchHistory, fetchWeather } from './composables/api'

const pools = ref([])
const historyData = ref([])
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
  if (!historyData.value.length) return []
  
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

const SHORT_DAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
const SLOT_MINUTES = 10
const SLOTS_PER_DAY = (24 * 60) / SLOT_MINUTES // 144

const CHART_COLORS = [
  '#0066cc', '#22c55e', '#eab308', '#ef4444', '#8b5cf6',
  '#ec4899', '#06b6d4', '#f97316', '#84cc16', '#64748b'
]

// Build all 1008 labels: "Mon 00:00", "Mon 00:10", ..., "Sun 23:50"
const WEEKDAY_SLOT_LABELS = (() => {
  const labels = []
  for (let d = 0; d < 7; d++) {
    for (let h = 0; h < 24; h++) {
      for (let m = 0; m < 60; m += SLOT_MINUTES) {
        labels.push(`${SHORT_DAYS[d]} ${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`)
      }
    }
  }
  return labels
})()

function hexToRgba(hex, alpha) {
  const r = parseInt(hex.slice(1, 3), 16)
  const g = parseInt(hex.slice(3, 5), 16)
  const b = parseInt(hex.slice(5, 7), 16)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

function buildWeekdayChartData() {
  const data = historyData.value
  const labels = WEEKDAY_SLOT_LABELS

  // Bucket: pool -> slotIndex -> [values]
  const poolSlots = {}
  data.forEach(item => {
    const date = new Date(item.timestamp)
    const dow = (date.getDay() + 6) % 7 // Monday=0 ... Sunday=6
    const minuteOfDay = date.getHours() * 60 + Math.floor(date.getMinutes() / SLOT_MINUTES) * SLOT_MINUTES
    const slotIndex = dow * SLOTS_PER_DAY + minuteOfDay / SLOT_MINUTES
    const utilization = Math.max(0, 100 - item.utility)

    if (!poolSlots[item.name]) poolSlots[item.name] = {}
    if (!poolSlots[item.name][slotIndex]) poolSlots[item.name][slotIndex] = []
    poolSlots[item.name][slotIndex].push(utilization)
  })

  const isSinglePool = selectedPool.value !== ''
  const datasets = []

  Object.entries(poolSlots).forEach(([name, slots], i) => {
    const color = CHART_COLORS[i % CHART_COLORS.length]

    const means = labels.map((_, idx) => {
      const vals = slots[idx]
      return vals && vals.length > 0 ? vals.reduce((a, b) => a + b, 0) / vals.length : null
    })

    if (isSinglePool) {
      const stddevs = labels.map((_, idx) => {
        const vals = slots[idx]
        if (!vals || vals.length < 2 || means[idx] === null) return 0
        const mean = means[idx]
        const variance = vals.reduce((sum, v) => sum + (v - mean) ** 2, 0) / vals.length
        return Math.sqrt(variance)
      })

      datasets.push({
        label: name + ' (lower)',
        data: means.map((m, idx) => m !== null ? Math.max(0, m - stddevs[idx]) : null),
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
        label: name + ' (upper)',
        data: means.map((m, idx) => m !== null ? Math.min(100, m + stddevs[idx]) : null),
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
      label: name,
      data: means,
      borderColor: color,
      tension: 0.3,
      fill: false,
      spanGaps: true,
      borderDash: isSinglePool ? [] : [5, 1]
    })
  })

  return { labels, datasets }
}

const chartData = computed(() => {
  if (!historyData.value.length) return { labels: [], datasets: [] }

  if (isWeekdayView.value) return buildWeekdayChartData()

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
    const fetchDays = isWeekday ? 90 : selectedDays.value

    const params = new URLSearchParams()
    if (selectedPool.value) params.set('pool', selectedPool.value)
    params.set('days', fetchDays)
    
    const weatherParams = new URLSearchParams()
    weatherParams.set('days', fetchDays)
    
    const [history, weather] = await Promise.all([
      fetchHistory(params.toString()),
      isWeekday ? Promise.resolve([]) : fetchWeather(weatherParams.toString())
    ])
    
    historyData.value = history
    weatherData.value = weather
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
})
</script>
