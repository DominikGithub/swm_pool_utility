<template>
  <div class="pool-card stats-card">
    <h3>Stats Data Coverage</h3>
    <div class="stats-list" v-if="stats">
      <div class="stats-row">
        <span class="stats-label">Coverage</span>
        <span class="stats-value">~{{ stats.weeks }} weeks</span>
      </div>
      <div class="stats-row">
        <span class="stats-label">Samples</span>
        <span class="stats-value">{{ formatNum(stats.total_samples) }}</span>
      </div>
      <div class="stats-row">
        <span class="stats-label">Last Update</span>
        <span class="stats-value">{{ formatUpdatedAt(stats.updated_at) }}</span>
      </div>
    </div>
    <div class="stats-empty" v-else>
      <span class="stats-value muted">--</span>
    </div>
  </div>
</template>

<script setup>
defineProps({
  stats: {
    type: Object,
    default: null
  }
})

function formatNum(val) {
  if (val == null) return '--'
  return Number(val).toLocaleString('de-DE')
}

function formatUpdatedAt(ts) {
  if (!ts) return '--'
  // The API now returns Berlin-local RFC3339 (e.g. "2026-04-06T10:30:00+02:00").
  // Handle both that and legacy "YYYY-MM-DD HH:MM:SS" formats for robustness.
  const normalised = ts.includes('T') ? ts : ts.replace(' ', 'T')
  const withTz = normalised.endsWith('Z') || normalised.match(/[+-]\d{2}:\d{2}$/) ? normalised : normalised + 'Z'
  const d = new Date(withTz)
  if (isNaN(d.getTime())) return ts
  return d.toLocaleString('de-DE', {
    timeZone: 'Europe/Berlin',
    day: '2-digit', month: '2-digit', year: 'numeric',
    hour: '2-digit', minute: '2-digit'
  })
}
</script>

<style scoped>
.stats-card:hover {
  background-color: #f8fafc;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.08);
}

.stats-card h3 {
  font-size: 0.75rem;
  color: #666;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 0.5rem;
}

.stats-list {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}

.stats-row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 0.5rem;
}

.stats-label {
  font-size: 0.7rem;
  color: #999;
  font-weight: 500;
  flex-shrink: 0;
}

.stats-value {
  font-size: 0.8rem;
  font-weight: 600;
  color: #333;
  text-align: right;
}

.stats-value.muted {
  font-size: 1.5rem;
  color: #d6d3d1;
}

.stats-empty {
  text-align: center;
  padding: 0.25rem 0;
}
</style>
