<template>
  <div class="chart-wrapper">
    <Line :data="data" :options="options" />
  </div>
</template>

<script setup>
import { computed } from 'vue'
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

const options = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  resizeDelay: 100,
  animation: {
    duration: 0
  },
  interaction: {
    mode: 'point',
    intersect: true
  },
  elements: {
    point: {
      radius: 4,
      hoverRadius: 8,
      hoverBorderWidth: 2,
      hitRadius: 15
    },
    line: {
      hitRadius: 15
    }
  },
  plugins: {
    legend: {
      position: 'bottom',
      labels: {
        boxWidth: 20,
        boxHeight: 3,
        padding: 15,
        font: { size: 12 },
        usePointStyle: true
      }
    },
    tooltip: {
      animation: false,
      filter: (tooltipItem) => tooltipItem.dataset.data.length > 0,
      callbacks: {
        label: (ctx) => `${ctx.dataset.label}: ${ctx.parsed.y}%`
      }
    }
  },
  scales: {
    y: {
      beginAtZero: true,
      max: 100,
      ticks: {
        callback: (v) => v + '%'
      },
      title: {
        display: true,
        text: 'Utilization',
        font: {
          size: 13,
          weight: '500',
          family: "'Segoe UI', system-ui, -apple-system, sans-serif"
        },
        color: '#4b5563',
        padding: { top: 10 }
      }
    },
    x: {
      type: 'category',
      ticks: {
        maxTicksLimit: 10,
        font: { size: 10 }
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
