<template>
  <div 
    class="chart-wrapper" 
    ref="wrapperRef"
    @mousemove="handleMouseMove"
    @mouseleave="handleMouseLeave"
    @touchstart.prevent="handleTouchStart"
    @touchmove.prevent="handleTouchMove"
    @touchend="handleTouchEnd"
  >
    <Line ref="chartRef" :data="localData" :options="localOptions" />
    <div class="weather-icons" v-if="props.weatherData && props.weatherData.length > 0">
      <div 
        v-for="(icon, index) in weatherIcons" 
        :key="index"
        class="weather-icon"
        :class="[icon.type, { 'high-wind': icon.highWind }]"
        :style="{ left: icon.x + '%', opacity: icon.opacity }"
        :title="icon.title"
      >
        <span v-if="icon.type === 'sun'">☀️</span>
        <span v-else-if="icon.type === 'partly-cloudy'">⛅</span>
        <span v-else-if="icon.type === 'cloudy'">☁️</span>
        <span v-else-if="icon.type === 'rain'">🌧️</span>
        <span v-else-if="icon.type === 'snow'">❄️</span>
        <span v-else-if="icon.type === 'thunderstorm'">⛈️</span>
      </div>
    </div>
    <div 
      v-if="isVisible" 
      class="crosshair" 
      :style="{ left: crosshairX + 'px' }"
    >
      <div class="crosshair-label">{{ hoverLabel }}</div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, computed, onMounted, nextTick } from 'vue'
import { Line } from 'vue-chartjs'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend
} from 'chart.js'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend)

const props = defineProps({
  data: {
    type: Object,
    required: true
  },
  weatherData: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['hoverData'])
const chartRef = ref(null)
const wrapperRef = ref(null)
const crosshairX = ref(0)
const hoverLabel = ref('')
const isVisible = ref(false)
const localData = ref(props.data)
const isTouching = ref(false)

const weatherIcons = computed(() => {
  if (!props.weatherData || props.weatherData.length === 0) return []
  
  const chart = chartRef.value?.chart
  if (!chart || !chart.scales?.x || !localData.value.labels || localData.value.labels.length === 0) return []
  
  const labels = localData.value.labels
  const xScale = chart.scales.x
  
  const chartLeft = xScale.left
  const chartRight = xScale.right
  const chartWidth = chartRight - chartLeft
  
  if (chartWidth <= 0) return []
  
  const wrapperWidth = wrapperRef.value?.offsetWidth || 1
  
  const icons = []
  const step = Math.max(1, Math.floor(labels.length / 10))
  
  for (let i = 0; i < labels.length; i += step) {
    const weather = findNearestWeather(labels[i])
    if (weather) {
      const normalizedPos = labels.length > 1 ? i / (labels.length - 1) : 0
      const pixelPos = chartLeft + normalizedPos * chartWidth
      const percentFromWrapper = (pixelPos / wrapperWidth) * 100
      
      const minPercent = (chartLeft / wrapperWidth) * 100 + 0.5
      const maxPercent = (chartRight / wrapperWidth) * 100 - 0.5
      
      if (percentFromWrapper > minPercent && percentFromWrapper < maxPercent) {
        icons.push({
          x: percentFromWrapper,
          type: weather.weather_type,
          highWind: weather.wind_speed > 15,
          opacity: 0.5,
          title: `${weather.temperature}°C, ${weather.wind_speed} km/h`
        })
      }
    }
  }
  
  return icons
})

function findNearestWeather(label, maxDiffMs = 45 * 60 * 1000) {
  if (!props.weatherData || props.weatherData.length === 0) return null
  
  const labelDate = parseChartLabel(label)
  if (!labelDate) return null
  
  let nearest = null
  let minDiff = Infinity
  
  props.weatherData.forEach(w => {
    const wDate = new Date(w.timestamp)
    const diff = Math.abs(wDate.getTime() - labelDate.getTime())
    if (diff < minDiff && diff < maxDiffMs) {
      minDiff = diff
      nearest = w
    }
  })
  
  return nearest
}

function parseChartLabel(label) {
  let match = label.match(/(\d{2})\.(\d{2})\.,\s*(\d{2}):(\d{2})/)
  if (match) {
    const now = new Date()
    const day = parseInt(match[1])
    const month = parseInt(match[2]) - 1
    const hour = parseInt(match[3])
    const minute = parseInt(match[4])
    return new Date(now.getFullYear(), month, day, hour, minute)
  }
  match = label.match(/(\d{2})\.(\d{2})\.(\d{4}),\s*(\d{2}):(\d{2})/)
  if (match) {
    const day = parseInt(match[1])
    const month = parseInt(match[2]) - 1
    const year = parseInt(match[3])
    const hour = parseInt(match[4])
    const minute = parseInt(match[5])
    return new Date(year, month, day, hour, minute)
  }
  return null
}

watch(() => props.data, (newData) => {
  localData.value = newData
  if (props.weatherData && props.weatherData.length > 0) {
    nextTick(() => updateWeatherDatasets(props.weatherData))
  }
}, { deep: true })

watch(() => props.weatherData, (newWeather) => {
  updateWeatherDatasets(newWeather || [])
}, { deep: true })

onMounted(() => {
  nextTick(() => {
    if (props.weatherData && props.weatherData.length > 0) {
      updateWeatherDatasets(props.weatherData)
    }
  })
})

function updateWeatherDatasets(weatherData) {
  const chart = chartRef.value?.chart
  if (!chart) return

  for (let i = chart.data.datasets.length - 1; i >= 0; i--) {
    if (chart.data.datasets[i]._weather) {
      chart.data.datasets.splice(i, 1)
    }
  }

  if (!weatherData || weatherData.length === 0) {
    chart.update()
    return
  }

  const labels = localData.value.labels || []
  if (labels.length === 0) return

  let lastTemp = null
  let lastWind = null
  const tempData = []
  const windData = []

  labels.forEach(label => {
    const weather = findNearestWeather(label, 360)
    if (weather) {
      lastTemp = weather.temperature
      lastWind = weather.wind_speed
    }
    if (lastTemp !== null) {
      const normalizedTemp = ((lastTemp + 10) / 45) * 100
      tempData.push({ x: label, y: Math.max(0, Math.min(100, normalizedTemp)) })
    }
    if (lastWind !== null) {
      const normalizedWind = (lastWind / 60) * 100
      windData.push({ x: label, y: Math.max(0, Math.min(100, normalizedWind)) })
    }
  })

  if (tempData.length > 0) {
    chart.data.datasets.push({
      label: 'Temperature',
      data: tempData,
      borderColor: 'rgba(255, 140, 0, 0.6)',
      backgroundColor: 'transparent',
      borderWidth: 2,
      borderDash: [5, 5],
      tension: 0.3,
      fill: false,
      pointRadius: 0,
      yAxisID: 'y',
      _weather: true
    })
  }

  if (windData.length > 0) {
    chart.data.datasets.push({
      label: 'Wind',
      data: windData,
      borderColor: 'rgba(100, 116, 139, 0.6)',
      backgroundColor: 'transparent',
      borderWidth: 2,
      borderDash: [5, 5],
      tension: 0.3,
      fill: false,
      pointRadius: 0,
      yAxisID: 'y',
      _weather: true
    })
  }

  chart.update()
}

function getChartX(clientX) {
  const chart = chartRef.value?.chart
  
  if (!chart || !chart.scales?.x || !wrapperRef.value) return null

  const wrapperRect = wrapperRef.value.getBoundingClientRect()
  const xAxis = chart.scales.x
  
  const chartLeft = wrapperRect.left + xAxis.left
  const chartRight = wrapperRect.left + xAxis.right
  
  if (clientX < chartLeft || clientX > chartRight) return null

  return clientX - wrapperRect.left
}

function updateHover(clientX) {
  const x = getChartX(clientX)
  
  if (x === null) {
    isVisible.value = false
    hoverLabel.value = ''
    emit('hoverData', null)
    return
  }

  const chart = chartRef.value?.chart
  if (!chart) return

  const labels = localData.value.labels
  const dataCount = labels?.length || 0
  if (dataCount === 0) {
    isVisible.value = false
    emit('hoverData', null)
    return
  }

  crosshairX.value = x
  isVisible.value = true

  const xAxis = chart.scales.x
  const wrapperRect = wrapperRef.value.getBoundingClientRect()
  const canvasRelativeX = clientX - wrapperRect.left
  const normalizedValue = (canvasRelativeX - xAxis.left) / (xAxis.right - xAxis.left)
  let dataIndex = Math.round(normalizedValue * (dataCount - 1))
  dataIndex = Math.max(0, Math.min(dataIndex, dataCount - 1))

  const fullLabel = labels[dataIndex]
  if (fullLabel) {
    const timePart = fullLabel.split(', ')[1] || fullLabel
    hoverLabel.value = timePart
  }

  const values = {}
  localData.value.datasets?.forEach(ds => {
    if (ds.data && ds.data[dataIndex] !== undefined) {
      const point = ds.data[dataIndex]
      values[ds.label] = typeof point === 'object' && point !== null ? point.y : point
    }
  })
  emit('hoverData', values)
}

function handleMouseMove(event) {
  if (!isTouching.value) {
    updateHover(event.clientX)
  }
}

function handleMouseLeave() {
  if (!isTouching.value) {
    isVisible.value = false
    hoverLabel.value = ''
    emit('hoverData', null)
  }
}

function handleTouchStart(event) {
  isTouching.value = true
  if (event.touches.length > 0) {
    updateHover(event.touches[0].clientX)
  }
}

function handleTouchMove(event) {
  if (event.touches.length > 0) {
    updateHover(event.touches[0].clientX)
  }
}

function handleTouchEnd() {
  isTouching.value = false
  isVisible.value = false
  hoverLabel.value = ''
  emit('hoverData', null)
}

const localOptions = {
  responsive: true,
  maintainAspectRatio: false,
  resizeDelay: 100,
  animation: {
    duration: 0
  },
  interaction: {
    mode: 'index',
    intersect: false
  },
  layout: {
    padding: {
      left: 20,
      right: 20,
      top: 10,
      bottom: 10
    }
  },
  plugins: {
    legend: {
      display: false
    },
    tooltip: {
      enabled: false
    }
  },
  elements: {
    point: {
      radius: 0,
      hoverRadius: 0
    },
    line: {
      tension: 0.3
    }
  },
  scales: {
    y: {
      beginAtZero: true,
      max: 100,
      padding: 8,
      ticks: {
        callback: (v) => v + '%',
        font: { size: 10 }
      }
    },
    x: {
      type: 'category',
      padding: 8,
      ticks: {
        maxTicksLimit: 8,
        font: { size: 9 },
        maxRotation: 0
      }
    }
  }
}
</script>

<style scoped>
.chart-wrapper {
  position: relative;
  flex: 1;
  min-height: 0;
  height: 100%;
  touch-action: none;
}

.weather-icons {
  position: absolute;
  top: 30%;
  left: 0;
  right: 0;
  height: 60%;
  pointer-events: none;
  z-index: 5;
}

.weather-icon {
  position: absolute;
  transform: translateX(-50%);
  font-size: 16px;
  text-shadow: 0 1px 2px rgba(255, 255, 255, 0.8);
}

.weather-icon.high-wind {
  font-size: 20px;
  opacity: 0.7;
}

.crosshair {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1.5px;
  background-color: rgba(0, 0, 0, 0.6);
  pointer-events: none;
  z-index: 10;
}

.crosshair-label {
  position: absolute;
  top: -2px;
  left: 50%;
  transform: translateX(-50%);
  background-color: rgba(0, 0, 0, 0.75);
  color: white;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 500;
  white-space: nowrap;
  pointer-events: none;
}
</style>
