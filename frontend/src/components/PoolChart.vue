<template>
  <div class="chart-wrapper" ref="wrapperRef">
    <canvas ref="canvasRef"></canvas>
    <div class="weather-icons" v-if="weatherIcons.length > 0">
      <div
        v-for="(icon, index) in weatherIcons"
        :key="index"
        class="weather-icon"
        :class="{ strong: icon.strong }"
        :style="{ left: icon.x + '%', opacity: icon.opacity }"
        :title="icon.title"
      >
        <span class="icon-main">{{ icon.emoji }}</span>
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
  },
  predictionTimestamps: {
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
  afterEvent(chart, args) {
    const event = args.event
    if (event.type === 'mousemove') {
      chart._crosshairX = event.x
    } else if (event.type === 'mouseout') {
      chart._crosshairX = null
    }
  },
  afterDraw(chart) {
    const x = chart._crosshairX
    if (x == null) return

    const xScale = chart.scales.x
    const yScale = chart.scales.y
    if (!xScale || !yScale) return
    if (x < xScale.left || x > xScale.right) return

    const ctx = chart.ctx
    ctx.save()

    // Draw vertical line at actual mouse position
    ctx.beginPath()
    ctx.moveTo(x, yScale.top)
    ctx.lineTo(x, yScale.bottom)
    ctx.lineWidth = 1
    ctx.strokeStyle = 'rgba(0, 0, 0, 0.5)'
    ctx.stroke()

    // Draw label at top of line: "Mon 14:30"
    //
    // IMPORTANT: do NOT use tooltip.dataPoints[0].label here. Chart.js mode:'index'
    // does not update the tooltip when no historical dataset has data at the hovered
    // position (e.g. the prediction area to the right of the last measured point).
    // The tooltip freezes at the last valid historical position, so the label would
    // show a stale past time (e.g. "12:30") while the crosshair line has already
    // moved to the prediction zone.
    //
    // Instead, derive the category label index directly from the pixel position via
    // the x-scale. This always reflects the actual mouse position, regardless of
    // whether a dataset has data there.
    const nLabels = chart.data.labels?.length ?? 0
    if (nLabels === 0) return

    const rawIdx = xScale.getValueForPixel(x)
    const labelIdx = Math.max(0, Math.min(Math.round(rawIdx), nLabels - 1))
    const label = chart.data.labels[labelIdx]

    if (label) {
      const timeMatch = label.match(/(\d{2}:\d{2})/)
      const timeStr = timeMatch ? timeMatch[1] : label
      const ts = chart.data.timestamps?.[labelIdx] ?? getTimestampForLabel(label)
      const dow = ts ? getDayOfWeek(ts) : null
      const dowPrefix = label.match(/^([A-Z][a-z]{2})\s/)
      const displayStr = dow ? `${dow.short} ${timeStr}` : (dowPrefix ? label : timeStr)

      ctx.font = '10px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif'
      ctx.textAlign = 'center'
      ctx.textBaseline = 'bottom'
      const textWidth = ctx.measureText(displayStr).width
      const padding = 4
      const boxWidth = textWidth + padding * 2
      const boxHeight = 16
      const boxY = yScale.top - boxHeight - 2

      // Clamp horizontally to stay within chart area
      let boxX = x - boxWidth / 2
      if (boxX < xScale.left) boxX = xScale.left
      if (boxX + boxWidth > xScale.right) boxX = xScale.right - boxWidth

      ctx.fillStyle = 'rgba(0, 0, 0, 0.75)'
      ctx.beginPath()
      ctx.roundRect(boxX, boxY, boxWidth, boxHeight, 3)
      ctx.fill()

      ctx.fillStyle = '#fff'
      ctx.textAlign = 'left'
      ctx.fillText(displayStr, boxX + padding, boxY + boxHeight - 3)
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

// Get the raw ISO timestamp for a given chart data index (used by iteration-based code).
function getTimestampAt(index) {
  return chart?.data?.timestamps?.[index] ?? null
}

// Look up the raw ISO timestamp for a chart label string.
// This is used by the crosshair / tooltip instead of index-based lookup, because
// tooltip.dataPoints[].dataIndex is the index within a dataset's data array —
// which may differ from the label index when datasets have different lengths.
//
// IMPORTANT: uses chart.data (not props.data) because Chart.js draw/interaction
// callbacks fire during Vue's async watch window — after props.data is updated but
// before the watch callback has synced chart.data. chart.data is always consistent
// with what is currently rendered.
function getTimestampForLabel(label) {
  if (!label || !chart?.data?.labels || !chart?.data?.timestamps) return null
  const idx = chart.data.labels.indexOf(label)
  return idx >= 0 ? chart.data.timestamps[idx] : null
}

function findNearestWeather(isoTimestamp, maxDiffMs = 45 * 60 * 1000) {
  if (!props.weatherData || props.weatherData.length === 0 || !isoTimestamp) return null

  const targetMs = new Date(isoTimestamp).getTime()
  if (isNaN(targetMs)) return null

  let nearest = null
  let minDiff = Infinity

  props.weatherData.forEach(w => {
    const diff = Math.abs(new Date(w.timestamp).getTime() - targetMs)
    if (diff < minDiff && diff < maxDiffMs) {
      minDiff = diff
      nearest = w
    }
  })

  return nearest
}

const DOW_FULL = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const DOW_SHORT = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

// Berlin-aware day of week from an ISO timestamp (e.g. "2026-04-06T10:30:00+02:00").
const berlinDowFormatter = typeof Intl !== 'undefined'
  ? new Intl.DateTimeFormat('en-US', { timeZone: 'Europe/Berlin', weekday: 'short' })
  : null

function getDayOfWeek(isoTimestamp) {
  if (!isoTimestamp) return null
  const d = new Date(isoTimestamp)
  if (isNaN(d.getTime())) return null

  if (berlinDowFormatter) {
    const short = berlinDowFormatter.format(d)
    const idx = DOW_SHORT.indexOf(short)
    return idx >= 0 ? { full: DOW_FULL[idx], short } : null
  }
  // Fallback (shouldn't happen in modern browsers)
  const dow = d.getDay()
  return { full: DOW_FULL[dow], short: DOW_SHORT[dow] }
}

const WEATHER_EMOJI = {
  clear: '',
  sunny: '☀️',
  partly_cloudy: '⛅',
  cloudy: '☁️',
  overcast: '☁️',
  rain: '🌧️',
  rainy: '🌧️',
  drizzle: '🌦️',
  snow: '❄️',
  snowy: '❄️',
  sleet: '🌨️',
  thunderstorm: '⛈️',
  fog: '🌫️',
  mist: '🌫️',
}

const HIGH_WIND_KMH = 15
const VERY_HIGH_WIND_KMH = 30
const MIN_ICON_SPACING = 4 // percent

function getWeatherEmoji(weatherType) {
  return WEATHER_EMOJI[weatherType] ?? '☁️'
}

function buildIconTitle(w) {
  const parts = [`${w.temperature}°C`, `${w.wind_speed} km/h wind`, `${w.cloud_cover}% clouds`]
  if (w.precipitation > 0) parts.push(`${w.precipitation} mm`)
  return parts.join(', ')
}

function updateWeatherIcons() {
  if (!chart || !chart.scales?.x || !props.data.labels || props.data.labels.length === 0
      || !props.weatherData || props.weatherData.length === 0) {
    weatherIcons.value = []
    return
  }

  const labels = props.data.labels
  const xScale = chart.scales.x
  const chartLeft = xScale.left
  const chartRight = xScale.right
  const chartWidth = chartRight - chartLeft
  if (chartWidth <= 0) { weatherIcons.value = []; return }
  const wrapperWidth = wrapperRef.value?.offsetWidth || 1

  const minX = (chartLeft / wrapperWidth) * 100 + 1
  const maxX = (chartRight / wrapperWidth) * 100 - 1

  // Map a weather ISO timestamp to a chart x-percent position.
  // Uses the raw ISO timestamps array (parallel to labels) for accurate matching.
  const timestamps = props.data.timestamps || []
  // Pre-compute millisecond values for all chart timestamps
  const tsMsArray = timestamps.map(t => new Date(t).getTime())

  function timestampToX(timestamp) {
    const wMs = new Date(timestamp).getTime()
    let bestIdx = 0, bestDiff = Infinity
    for (let i = 0; i < tsMsArray.length; i++) {
      const diff = Math.abs(tsMsArray[i] - wMs)
      if (diff < bestDiff) { bestDiff = diff; bestIdx = i }
    }
    const norm = labels.length > 1 ? bestIdx / (labels.length - 1) : 0
    return ((chartLeft + norm * chartWidth) / wrapperWidth) * 100
  }

  const events = []
  let prevType = null
  let prevWindHigh = false

  for (let i = 0; i < props.weatherData.length; i++) {
    const w = props.weatherData[i]
    const type = w.weather_type
    const windHigh = w.wind_speed >= HIGH_WIND_KMH
    const windVeryHigh = w.wind_speed >= VERY_HIGH_WIND_KMH

    const isFirst = i === 0
    const typeChanged = prevType !== null && type !== prevType
    const windSpiked = windHigh && !prevWindHigh

    if (isFirst || typeChanged || windSpiked) {
      const x = timestampToX(w.timestamp)
      if (x >= minX && x <= maxX) {
        // Single emoji: weather change takes priority; pure wind spike shows wind icon
        let emoji
        if (isFirst || typeChanged) {
          emoji = getWeatherEmoji(type)
        } else {
          emoji = windVeryHigh ? '🌬️' : '💨'
        }
        events.push({
          x,
          emoji,
          strong: windVeryHigh,
          title: buildIconTitle(w),
          opacity: 0.6,
        })
      }
    }

    prevType = type
    prevWindHigh = windHigh
  }

  // Merge events that land too close together on the x-axis
  const merged = []
  for (const ev of events) {
    const last = merged[merged.length - 1]
    if (last && Math.abs(ev.x - last.x) < MIN_ICON_SPACING) {
      last.emoji = ev.emoji
      if (ev.strong) last.strong = true
      last.title = ev.title
    } else {
      merged.push({ ...ev })
    }
  }

  weatherIcons.value = merged.filter(ev => ev.emoji)
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
          left: 0,
          right: 12,
          top: 18,
          bottom: 10
        }
      },
      plugins: {
        legend: {
          display: false
        },
        tooltip: {
          enabled: false,
          filter: function(tooltipItem) {
            return !tooltipItem.dataset._weather && !tooltipItem.dataset._ci
          },
          external: function(context) {
            const tooltip = context.tooltip
            
            if (tooltip.opacity === 0) {
              emit('hoverData', null, null)
              return
            }

            // parsed.x is the Chart.js integer category-label index — valid for both
            // dense historical datasets and sparse prediction datasets, unlike
            // dataIndex which is dataset-local and gives wrong positions when datasets
            // have different lengths.
            const labelIdx = tooltip.dataPoints?.[0]?.parsed?.x ?? -1
            
            if (labelIdx >= 0) {
              const values = {}
              chart.data.datasets?.forEach(ds => {
                if (ds._weather || ds._ci) return
                if (ds.data && ds.data[labelIdx] !== undefined) {
                  const point = ds.data[labelIdx]
                  values[ds.label] = typeof point === 'object' && point !== null ? point.y : point
                }
              })

              const label = tooltip.dataPoints?.[0]?.label
              const ts = (chart.data.timestamps?.[labelIdx] != null)
                ? chart.data.timestamps[labelIdx]
                : getTimestampForLabel(label)
              const weather = ts ? findNearestWeather(ts) : null

              emit('hoverData', values, { index: labelIdx, weather })
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
          afterBuildTicks(scale) {
            const labels = scale.chart.data.labels || []
            const firstLabel = labels[0] || ''
            if (firstLabel.match(/^[A-Z][a-z]{2}\s\d{2}:\d{2}$/)) {
              const slotsPerDay = Math.round(labels.length / 7)
              const noonSlot = Math.round(slotsPerDay / 2)
              scale.ticks = []
              for (let d = 0; d < 7; d++) {
                scale.ticks.push({ value: d * slotsPerDay + noonSlot })
              }
            }
          },
          ticks: {
            maxTicksLimit: 8,
            font: { size: 9 },
            maxRotation: 0,
            callback: function(val) {
              const label = this.getLabelForValue(val)
              const dowMatch = label.match(/^([A-Z][a-z]{2})\s\d{2}:\d{2}$/)
              if (dowMatch) {
                const full = { Mon: 'Monday', Tue: 'Tuesday', Wed: 'Wednesday', Thu: 'Thursday', Fri: 'Friday', Sat: 'Saturday', Sun: 'Sunday' }
                return full[dowMatch[1]] || dowMatch[1]
              }
              return label
            }
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

  const timestamps = props.data.timestamps || []
  let lastTemp = null
  const tempData = []

  labels.forEach((label, index) => {
    const ts = timestamps[index]
    const weather = ts ? findNearestWeather(ts) : null
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
      backgroundColor: 'rgba(255, 170, 60, 0.10)',
      borderWidth: 0,
      tension: 0.3,
      fill: 'origin',
      pointRadius: 0,
      pointHoverRadius: 0,
      pointHoverBorderWidth: 0,
      pointHitRadius: 0,
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
  top: 12px;
  left: 0;
  right: 0;
  height: 30%;
  pointer-events: none;
  z-index: 5;
}

.weather-icon {
  position: absolute;
  top: 50%;
  transform: translateX(-50%) translateY(-50%);
  line-height: 1;
}

.icon-main {
  font-size: 30px;
  filter: drop-shadow(0 1px 2px rgba(255,255,255,0.95));
}

.weather-icon.strong .icon-main {
  font-size: 36px;
}
</style>
