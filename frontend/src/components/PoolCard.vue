<template>
  <div class="pool-card">
    <h3>{{ pool.name }}</h3>
    <div class="value" :class="levelClass">{{ pool.utility }}%</div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  pool: {
    type: Object,
    required: true
  }
})

const levelClass = computed(() => {
  const v = props.pool.utility
  if (v < 40) return 'low'
  if (v < 70) return 'medium'
  return 'high'
})
</script>
