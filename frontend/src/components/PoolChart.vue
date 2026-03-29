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
import { ref, watch } from 'vue'
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

watch(() => props.data, (newData) => {
  localData.value = newData
}, { deep: true })

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

  const dataCount = chart.data.labels?.length || 0
  if (dataCount === 0) {
    isVisible.value = false
    emit('hoverData', null)
    return
  }

  crosshairX.value = x
  isVisible.value = true

  const xAxis = chart.scales.x
  const canvas = chart.canvas
  const canvasRect = canvas.getBoundingClientRect()
  const canvasRelativeX = clientX - canvasRect.left
  const normalizedValue = (canvasRelativeX - xAxis.left) / xAxis.width
  let dataIndex = Math.round(normalizedValue * (dataCount - 1))
  dataIndex = Math.max(0, Math.min(dataIndex, dataCount - 1))

  if (chart.data.labels && chart.data.labels[dataIndex]) {
    const fullLabel = chart.data.labels[dataIndex]
    const timePart = fullLabel.split(', ')[1] || fullLabel
    hoverLabel.value = timePart
  }

  const values = {}
  localData.value.datasets?.forEach(ds => {
    if (ds.data && ds.data[dataIndex]) {
      values[ds.label] = ds.data[dataIndex].y
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
