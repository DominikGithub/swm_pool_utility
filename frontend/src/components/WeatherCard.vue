<template>
  <div class="pool-card weather-card">
    <h3>Weather</h3>
    <div class="weather-grid" v-if="weather">
      <div class="weather-item">
        <span class="weather-label">Temp</span>
        <span class="weather-value">{{ formatTemp(weather.temperature) }}</span>
      </div>
      <div class="weather-item">
        <span class="weather-label">Wind</span>
        <span class="weather-value" :class="{ 'high-wind': weather.wind_speed > 15 }">{{ formatNum(weather.wind_speed) }} km/h</span>
      </div>
      <div class="weather-item">
        <span class="weather-label">Clouds</span>
        <span class="weather-value">{{ formatNum(weather.cloud_cover) }}%</span>
      </div>
      <div class="weather-item">
        <span class="weather-label">Precip</span>
        <span class="weather-value">{{ formatNum(weather.precipitation) }} mm</span>
      </div>
    </div>
    <div class="weather-empty" v-else>
      <span class="weather-value muted">--</span>
    </div>
  </div>
</template>

<script setup>
defineProps({
  weather: {
    type: Object,
    default: null
  }
})

function formatTemp(val) {
  if (val == null) return '--'
  return Number(val).toFixed(1) + '\u00B0C'
}

function formatNum(val) {
  if (val == null) return '--'
  const n = Number(val)
  return Number.isInteger(n) ? String(n) : n.toFixed(1)
}
</script>

<style scoped>
.weather-card:hover {
  background-color: #f8fafc;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.08);
}

.weather-card h3 {
  font-size: 0.75rem;
  color: #666;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 0.5rem;
}

.weather-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.3rem 0.5rem;
}

.weather-item {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 0.25rem;
  min-width: 0;
}

.weather-label {
  font-size: 0.7rem;
  color: #999;
  font-weight: 500;
  flex-shrink: 0;
}

.weather-value {
  font-size: 0.8rem;
  font-weight: 600;
  color: #333;
  white-space: nowrap;
  text-align: right;
  overflow: hidden;
  text-overflow: ellipsis;
}

.weather-value.high-wind {
  color: #dc2626;
}

.weather-value.muted {
  font-size: 1.5rem;
  color: #d6d3d1;
}

.weather-empty {
  text-align: center;
  padding: 0.25rem 0;
}
</style>
