<template>
  <div class="pool-card" :class="{ favorite: isFavorite }">
    <div class="card-header">
      <h3>{{ pool.name }}</h3>
      <div class="header-right">
        <span v-if="status" class="arrow" :class="status.arrow" :title="`Predicted: ${Math.round(status.predicted)}%`">
          {{ arrowSymbol }}
        </span>
        <button class="star-btn" :class="{ active: isFavorite }" @click.stop="$emit('toggleFavorite')">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
            <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/>
          </svg>
        </button>
      </div>
    </div>
    <div class="value" :class="levelClass">{{ Math.round(pool.utility) }}%</div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  pool: {
    type: Object,
    required: true
  },
  isFavorite: {
    type: Boolean,
    default: false
  },
  status: {
    type: Object,
    default: null
  }
})

defineEmits(['toggleFavorite'])

const ARROWS = { up: '↑', down: '↓', stable: '→' }
const arrowSymbol = computed(() => props.status ? (ARROWS[props.status.arrow] || '') : '')

const levelClass = computed(() => {
  const v = props.pool.utility
  if (v < 40) return 'low'
  if (v < 70) return 'medium'
  return 'high'
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 0.5rem;
}

.card-header h3 {
  flex: 1;
  min-width: 0;
  overflow-wrap: break-word;
  hyphens: none;
  white-space: normal;
  word-break: keep-all;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 4px;
}

.arrow {
  font-size: 18px;
  line-height: 1;
  font-weight: 700;
  width: 22px;
  text-align: center;
  border-radius: 4px;
  padding: 1px 2px;
}

.arrow.up {
  color: #16a34a;
}

.arrow.down {
  color: #dc2626;
}

.arrow.stable {
  color: #9ca3af;
}

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
