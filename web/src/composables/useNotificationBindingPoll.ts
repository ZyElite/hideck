import { onBeforeUnmount, onMounted, watch, type MaybeRefOrGetter, toValue } from 'vue'

export function useNotificationBindingPoll(options: {
  shouldPoll: MaybeRefOrGetter<boolean>
  refresh: () => Promise<unknown>
  intervalMs?: number
}) {
  let timer: number | null = null
  const intervalMs = options.intervalMs ?? 2000

  function stop() {
    if (timer == null) return
    window.clearInterval(timer)
    timer = null
  }

  function start() {
    stop()
    if (!toValue(options.shouldPoll)) return
    timer = window.setInterval(() => {
      if (!toValue(options.shouldPoll)) {
        stop()
        return
      }
      void options.refresh()
    }, intervalMs)
  }

  onMounted(start)
  onBeforeUnmount(stop)
  watch(() => toValue(options.shouldPoll), start)

  return { restart: start, stop }
}
