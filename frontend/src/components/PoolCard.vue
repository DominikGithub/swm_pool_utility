<template>
  <div class="pool-card" :class="{ favorite: isFavorite }">
    <div class="card-body">
      <div class="card-left">
        <h3>{{ pool.name }}</h3>
        <div class="value" :class="levelClass">{{ Math.round(pool.utility) }}%</div>
      </div>
      <div class="card-right">
        <button class="star-btn" :class="{ active: isFavorite }" @click.stop="$emit('toggleFavorite')">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
            <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/>
          </svg>
        </button>
        <div
          v-if="status && status.arrow !== 'stable'"
          class="trend-indicator"
          :class="status.arrow"
          :title="`Trend: ${status.delta_1h > 0 ? '+' : ''}${status.delta_1h}% in 1h (strength: ${status.trend_strength.toFixed(1)})`"
        >
          <div class="arrow-circle">
            <span class="arrow-icon" :class="status.arrow">↑</span>
          </div>
          <div class="strength-bar">
            <div class="strength-fill" :style="{ width: Math.min(100, status.trend_strength * 5) + '%' }"></div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  pool: { type: Object, required: true },
  isFavorite: { type: Boolean, default: false },
  status: { type: Object, default: null }
})

defineEmits(['toggleFavorite'])

const levelClass = computed(() => {
  const v = props.pool.utility
  if (v < 40) return 'low'
  if (v < 70) return 'medium'
  return 'high'
})
</script>

<style scoped>
.card-body {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 0.5rem;
}

.card-left {
  flex: 1;
  min-width: 0;
}

.card-left h3 {
  overflow-wrap: break-word;
  hyphens: none;
  white-space: normal;
  word-break: keep-all;
  margin: 0;
}

.card-right {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  width: 28px;
}

.trend-indicator {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
}

.arrow-circle {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: #ffffff;
  border: 1px solid #1a1a1a;
  display: flex;
  align-items: center;
  justify-content: center;
}

.arrow-icon {
  font-size: 16px;
  line-height: 1;
  font-weight: 900;
}

.arrow-icon.up {
  color: #16a34a;
  transform: rotate(45deg);
  display: inline-block;
}

.arrow-icon.down {
  color: #dc2626;
  transform: rotate(135deg);
  display: inline-block;
}

.strength-bar {
  width: 100%;
  height: 4px;
  background: #e5e7eb;
  border-radius: 2px;
  overflow: hidden;
}

.strength-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.3s ease;
}

.trend-indicator.up .strength-fill { background: #16a34a; }
.trend-indicator.down .strength-fill { background: #dc2626; }

.star-btn {
  background: white;
  border: 1px solid #e5e7eb;
  padding: 2px;
  cursor: pointer;
  color: #d1d5db;
  transition: all 0.2s ease;
  flex-shrink: 0;
  border-radius: 4px;
  line-height: 1;
  box-shadow: none;
  width: 26px;
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.star-btn:hover {
  color: #fbbf24;
  border-color: #fbbf24;
}

.star-btn.active {
  color: #fbbf24;
  border-color: #fbbf24;
}
</style>
