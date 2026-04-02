<template>
  <div class="chart-wrapper" ref="wrapperRef">
    <canvas ref="canvasRef"></canvas>
    <div class="weather-icons" v-if="weatherIcons.length > 0">
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
  </div>
</template>

<script setup>
import { ref, watch, onMounted, nextTick, onBeforeUnmount } from 'vue'
import {
  Chart,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  LineController,
  Title,
  Tooltip,
  Filler
} from 'chart.js'

Chart.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  LineController,
  Title,
  Tooltip,
  Filler
)

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
const canvasRef = ref(null)
const wrapperRef = ref(null)
const weatherIcons = ref([])

let chart = null

const crosshairPlugin = {
  id: 'crosshair',
  afterDraw(chart) {
    const tooltip = chart.tooltip
    if (!tooltip || tooltip.opacity === 0) return

    const x = tooltip.caretX
    const yScale = chart.scales.y
    if (!yScale) return

    const ctx = chart.ctx
    ctx.save()

    // Draw vertical line
    ctx.beginPath()
    ctx.moveTo(x, yScale.top)
    ctx.lineTo(x, yScale.bottom)
    ctx.lineWidth = 1
    ctx.strokeStyle = 'rgba(0, 0, 0, 0.5)'
    ctx.stroke()

    // Draw time-only label at top of line
    const label = tooltip.dataPoints?.[0]?.label
    if (label) {
      const timeMatch = label.match(/(\d{2}:\d{2})/)
      const timeStr = timeMatch ? timeMatch[1] : label

      ctx.font = '10px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif'
      ctx.textAlign = 'center'
      ctx.textBaseline = 'bottom'
      const textWidth = ctx.measureText(timeStr).width
      const padding = 4
      const boxWidth = textWidth + padding * 2
      const boxHeight = 16
      const boxY = yScale.top - boxHeight - 2

      // Clamp horizontally to stay within chart area
      const xScale = chart.scales.x
      let boxX = x - boxWidth / 2
      if (xScale) {
        if (boxX < xScale.left) boxX = xScale.left
        if (boxX + boxWidth > xScale.right) boxX = xScale.right - boxWidth
      }

      ctx.fillStyle = 'rgba(0, 0, 0, 0.75)'
      ctx.beginPath()
      ctx.roundRect(boxX, boxY, boxWidth, boxHeight, 3)
      ctx.fill()

      ctx.fillStyle = '#fff'
      ctx.textAlign = 'left'
      ctx.fillText(timeStr, boxX + padding, boxY + boxHeight - 3)
    }

    ctx.restore()
  }
}

const tempLabelPlugin = {
  id: 'tempLabel',
  afterDraw(chart) {
    const tempDs = chart.data.datasets.find(ds => ds._weather && ds.label === 'Temperature')
    if (!tempDs || !tempDs.data || tempDs.data.length === 0) return

    const yScale = chart.scales.y
    const xScale = chart.scales.x
    if (!yScale || !xScale) return

    const ctx = chart.ctx
    ctx.save()
    ctx.font = '9px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif'
    ctx.fillStyle = 'rgba(200, 120, 20, 0.45)'
    ctx.textAlign = 'right'
    ctx.textBaseline = 'bottom'
    ctx.fillText('Temperature', xScale.right - 4, yScale.bottom - 4)
    ctx.restore()
  }
}

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

function updateWeatherIcons() {
  if (!chart || !chart.scales?.x || !props.data.labels || props.data.labels.length === 0) {
    weatherIcons.value = []
    return
  }
  
  const labels = props.data.labels
  const xScale = chart.scales.x
  const chartLeft = xScale.left
  const chartRight = xScale.right
  const chartWidth = chartRight - chartLeft
  
  if (chartWidth <= 0) {
    weatherIcons.value = []
    return
  }
  
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
        let iconType = weather.weather_type
        let title = `${weather.temperature}°C, ${weather.wind_speed} km/h, ${weather.cloud_cover}% clouds`
        
        if (weather.cloud_cover > 70 && hasHighCloudsForHours(i, labels, 3)) {
          iconType = 'cloudy'
          title += ' (overcast 3+ hours)'
        }
        
        icons.push({
          x: percentFromWrapper,
          type: iconType,
          highWind: weather.wind_speed > 15,
          opacity: 0.5,
          title: title
        })
      }
    }
  }
  
  weatherIcons.value = icons
}

function hasHighCloudsForHours(index, labels, minHours) {
  let count = 0
  for (let i = index; i < labels.length; i++) {
    const weather = findNearestWeather(labels[i], 360)
    if (weather && weather.cloud_cover > 70) {
      count++
    } else {
      break
    }
  }
  return count >= minHours
}

function createChart() {
  if (!canvasRef.value || !props.data.labels) return

  const ctx = canvasRef.value.getContext('2d')
  
  if (chart) {
    chart.destroy()
  }

  chart = new Chart(ctx, {
    type: 'line',
    data: JSON.parse(JSON.stringify(props.data)),
    plugins: [crosshairPlugin, tempLabelPlugin],
    options: {
      responsive: true,
      maintainAspectRatio: false,
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
          top: 24,
          bottom: 10
        }
      },
      plugins: {
        legend: {
          display: false
        },
        tooltip: {
          enabled: false,
          external: function(context) {
            const tooltip = context.tooltip
            
            if (tooltip.opacity === 0) {
              emit('hoverData', null, null)
              return
            }

            const dataIndex = tooltip.dataPoints?.[0]?.dataIndex ?? -1
            
            if (dataIndex >= 0) {
              const values = {}
              chart.data.datasets?.forEach(ds => {
                if (ds.data && ds.data[dataIndex] !== undefined) {
                  const point = ds.data[dataIndex]
                  values[ds.label] = typeof point === 'object' && point !== null ? point.y : point
                }
              })

              const label = tooltip.dataPoints?.[0]?.label
              const weather = label ? findNearestWeather(label) : null

              emit('hoverData', values, { index: dataIndex, weather })
            }
          }
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
  })

  updateWeatherData()
  updateWeatherIcons()
}

function updateWeatherData() {
  if (!chart) return

  for (let i = chart.data.datasets.length - 1; i >= 0; i--) {
    if (chart.data.datasets[i]._weather) {
      chart.data.datasets.splice(i, 1)
    }
  }

  if (!props.weatherData || props.weatherData.length === 0) {
    chart.update()
    return
  }

  const labels = props.data.labels || []
  if (labels.length === 0) return

  let lastTemp = null
  const tempData = []

  labels.forEach(label => {
    const weather = findNearestWeather(label, 360)
    if (weather) {
      lastTemp = weather.temperature
    }
    if (lastTemp !== null) {
      const normalizedTemp = ((lastTemp + 10) / 45) * 100
      tempData.push({ x: label, y: Math.max(0, Math.min(100, normalizedTemp)) })
    }
  })

  if (tempData.length > 0) {
    chart.data.datasets.unshift({
      type: 'line',
      label: 'Temperature',
      data: tempData,
      borderColor: 'transparent',
      backgroundColor: 'rgba(255, 170, 60, 0.08)',
      borderWidth: 0,
      tension: 0.3,
      fill: 'origin',
      pointRadius: 0,
      pointHoverRadius: 0,
      pointHoverBorderWidth: 0,
      yAxisID: 'y',
      order: 10,
      _weather: true
    })
  }

  chart.update()
}

watch(() => props.data, (newData) => {
  if (chart) {
    chart.data = JSON.parse(JSON.stringify(newData))
    chart.update()
  } else {
    nextTick(() => createChart())
  }
  nextTick(() => {
    updateWeatherData()
    updateWeatherIcons()
  })
})

watch(() => props.weatherData, (newWeather) => {
  nextTick(() => {
    updateWeatherData()
    updateWeatherIcons()
  })
})

onMounted(() => {
  nextTick(() => createChart())
})

onBeforeUnmount(() => {
  if (chart) {
    chart.destroy()
    chart = null
  }
})
</script>

<style scoped>
.chart-wrapper {
  position: relative;
  flex: 1;
  min-height: 0;
  height: 100%;
}

.chart-wrapper canvas {
  display: block;
  width: 100%;
  height: 100%;
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
  font-size: 32px;
  text-shadow: 0 1px 2px rgba(255, 255, 255, 0.8);
}

.weather-icon.high-wind {
  font-size: 40px;
  opacity: 0.7;
}
</style>
