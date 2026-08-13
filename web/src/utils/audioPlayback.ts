export function formatPlaybackTime(value: number) {
  const seconds = Number.isFinite(value) && value > 0 ? Math.floor(value) : 0
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const remainder = seconds % 60
  if (hours > 0) return `${hours}:${padTime(minutes)}:${padTime(remainder)}`
  return `${minutes}:${padTime(remainder)}`
}

export function playbackProgress(currentTime: number, duration: number) {
  if (!Number.isFinite(currentTime) || !Number.isFinite(duration) || duration <= 0) return 0
  return Math.min(100, Math.max(0, (currentTime / duration) * 100))
}

function padTime(value: number) {
  return String(value).padStart(2, '0')
}
