<template>
  <div class="pool-card" :class="{ hovered: isHovered, favorite: isFavorite }">
    <div class="card-header">
      <h3>{{ pool.name }}</h3>
      <button class="star-btn" :class="{ active: isFavorite }" @click.stop="$emit('toggleFavorite')">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
          <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/>
        </svg>
      </button>
    </div>
    <div class="value" :class="levelClass">{{ pool.utility }}%</div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  pool: {
    type: Object,
    required: true
  },
  isHovered: {
    type: Boolean,
    default: false
  },
  isFavorite: {
    type: Boolean,
    default: false
  }
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
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 0.5rem;
}

.card-header h3 {
  flex: 1;
  min-width: 0;
}

.star-btn {
  background: none;
  border: none;
  padding: 0;
  cursor: pointer;
  color: #d1d5db;
  transition: color 0.2s ease, transform 0.2s ease;
  flex-shrink: 0;
  filter: none;
  box-shadow: none;
  border-radius: 0;
}

.star-btn:hover {
  color: #fbbf24;
  transform: scale(1.1);
}

.star-btn.active {
  color: #fbbf24;
}

.star-btn.active:hover {
  transform: scale(1.1);
}
</style>
