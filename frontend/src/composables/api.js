const API_BASE = '/api'

// Coerce a null/undefined API response to an empty array.
function toArray(value) {
  return Array.isArray(value) ? value : []
}

export async function fetchPools() {
  const res = await fetch(`${API_BASE}/pools`)
  return toArray(await res.json())
}

export async function fetchHistory(query = '') {
  const res = await fetch(`${API_BASE}/history?${query}`)
  return toArray(await res.json())
}

export async function fetchWeather(query = '') {
  const res = await fetch(`${API_BASE}/weather?${query}`)
  return toArray(await res.json())
}

export async function fetchDailyAvg(pool = '') {
  const qs = pool ? `pool=${encodeURIComponent(pool)}` : ''
  const res = await fetch(`${API_BASE}/daily-avg${qs ? '?' + qs : ''}`)
  // daily-avg returns an object (labels + datasets), not an array — pass through as-is.
  return res.json()
}

export async function fetchHourlyAvg(pool = '') {
  const qs = pool ? `pool=${encodeURIComponent(pool)}` : ''
  const res = await fetch(`${API_BASE}/hourly-avg${qs ? '?' + qs : ''}`)
  return toArray(await res.json())
}

export async function fetchPredictions(pool = '', hours = 6) {
  const params = new URLSearchParams()
  if (pool) params.set('pool', pool)
  params.set('hours', hours)
  const res = await fetch(`${API_BASE}/predictions?${params.toString()}`)
  return toArray(await res.json())
}

export async function fetchPoolStatus() {
  const res = await fetch(`${API_BASE}/pool-status`)
  return toArray(await res.json())
}
