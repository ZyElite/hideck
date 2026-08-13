export type TimelineScrollMetrics = Readonly<{
  scrollTop: number
  clientHeight: number
  scrollHeight: number
}>

export const TIMELINE_FOLLOW_THRESHOLD_PX = 64

export function isNearTimelineEnd(
  metrics: TimelineScrollMetrics,
  threshold = TIMELINE_FOLLOW_THRESHOLD_PX
) {
  const remainingDistance = metrics.scrollHeight - metrics.clientHeight - metrics.scrollTop
  return remainingDistance <= threshold
}

export function countAddedTimelineRecords(previousKeys: readonly string[], nextKeys: readonly string[]) {
  const previous = new Set(previousKeys)
  return nextKeys.reduce((count, key) => count + (previous.has(key) ? 0 : 1), 0)
}
