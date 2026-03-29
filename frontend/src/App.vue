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
        <option :value="7">Last 7 days</option>
        <option :value="14">Last 14 days</option>
        <option :value="30">Last 30 days</option>
        <option :value="90">Last 90 days</option>
        <option :value="0">All data</option>
      </select>
      <button @click="fetchData">Refresh</button>
    </div>

    <div v-if="loading" class="loading">Loading...</div>
    <template v-else>
      <div class="chart-container">
        <PoolChart :data="chartData" @hoverData="hoverData = $event" />
      </div>
      
      <div class="pool-list">
        <PoolCard 
          v-for="pool in currentPools" 
          :key="pool.name" 
          :pool="getPoolWithValue(pool)"
          :isFavorite="favorite === pool.name"
          @toggleFavorite="toggleFavorite(pool.name)"
        />
      </div>
      
    </template>
  </main>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import PoolChart from './components/PoolChart.vue'
import PoolCard from './components/PoolCard.vue'
import { fetchPools, fetchHistory } from './composables/api'

const pools = ref([])
const historyData = ref([])
const selectedPool = ref('')
const selectedDays = ref(1)
const loading = ref(true)
const favorite = ref('')
const hoverData = ref(null)

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

const chartData = computed(() => {
  if (!historyData.value.length) return { labels: [], datasets: [] }

  const days = selectedDays.value
  let filtered = historyData.value
  
  if (days > 0) {
    const cutoff = new Date()
    cutoff.setDate(cutoff.getDate() - days)
    filtered = historyData.value.filter(d => new Date(d.timestamp) >= cutoff)
  }
  
  const poolGroups = {}
  filtered.forEach(item => {
    if (!poolGroups[item.name]) poolGroups[item.name] = []
    const utilization = Math.max(0, 100 - item.utility)
    poolGroups[item.name].push({ x: formatTimestamp(item.timestamp), y: utilization })
  })

  const colors = [
    '#0066cc', '#22c55e', '#eab308', '#ef4444', '#8b5cf6',
    '#ec4899', '#06b6d4', '#f97316', '#84cc16', '#64748b'
  ]
  
  const datasets = Object.entries(poolGroups).map(([name, items], i) => ({
    label: name,
    data: items,
    borderColor: colors[i % colors.length],
    tension: 0.3,
    fill: false
  }))

  return { datasets }
})

async function fetchData() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (selectedPool.value) params.set('pool', selectedPool.value)
    params.set('days', selectedDays.value)
    
    historyData.value = await fetchHistory(params.toString())
  } catch (err) {
    console.error('Failed to fetch data:', err)
  }
  loading.value = false
}

onMounted(async () => {
  pools.value = await fetchPools()
  favorite.value = getCookie('swm_favorite') || ''
  selectedPool.value = favorite.value
  await fetchData()
})
</script>
