const API_BASE = '/api'

export async function fetchPools() {
  const res = await fetch(`${API_BASE}/pools`)
  return res.json()
}

export async function fetchHistory(query = '') {
  const res = await fetch(`${API_BASE}/history?${query}`)
  return res.json()
}

export async function fetchWeather(query = '') {
  const res = await fetch(`${API_BASE}/weather?${query}`)
  return res.json()
}
