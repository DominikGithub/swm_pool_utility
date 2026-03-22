<template>
  <div class="chart-wrapper">
    <Line ref="chartRef" :data="data" :options="options" />
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
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

const emit = defineEmits(['hover', 'leave'])
const chartRef = ref(null)

let lastHoveredDataset = null

watch(() => props.data, () => {
  if (chartRef.value?.chart) {
    const chart = chartRef.value.chart
    
    chart.data.datasets.forEach((dataset, datasetIndex) => {
      const meta = chart.getDatasetMeta(datasetIndex)
      if (meta.dataset) {
        meta.dataset.options = meta.dataset.options || {}
      }
    })
    chart.update('none')
  }
}, { deep: true })

const options = computed(() => ({
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
  onHover: (event, elements) => {
    if (elements.length > 0) {
      const datasetIndex = elements[0].datasetIndex
      const poolName = props.data.datasets[datasetIndex]?.label
      if (poolName && poolName !== lastHoveredDataset) {
        lastHoveredDataset = poolName
        emit('hover', poolName)
      }
    } else {
      if (lastHoveredDataset !== null) {
        lastHoveredDataset = null
        emit('leave')
      }
    }
  },
  elements: {
    point: {
      radius: 3,
      hoverRadius: 6,
      hoverBorderWidth: 2,
      hitRadius: 10
    },
    line: {
      hitRadius: 10
    }
  },
  plugins: {
    legend: {
      position: 'bottom',
      display: false,
      labels: {
        boxWidth: 12,
        boxHeight: 2,
        padding: 10,
        font: { size: 10 },
        usePointStyle: true
      }
    },
    tooltip: {
      animation: false,
      filter: (tooltipItem) => tooltipItem.dataset.data.length > 0,
      callbacks: {
        title: (items) => items[0]?.label || '',
        label: (ctx) => `${ctx.dataset.label}: ${ctx.parsed.y}%`
      }
    }
  },
  scales: {
    y: {
      beginAtZero: true,
      max: 100,
      ticks: {
        callback: (v) => v + '%',
        font: { size: 10 }
      },
      title: {
        display: false
      }
    },
    x: {
      type: 'category',
      ticks: {
        maxTicksLimit: 6,
        font: { size: 9 },
        maxRotation: 45
      }
    }
  }
}))
</script>

<style scoped>
.chart-wrapper {
  position: relative;
  flex: 1;
  min-height: 0;
  height: 100%;
}
</style>
