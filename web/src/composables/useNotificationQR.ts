import { computed, onBeforeUnmount, ref } from 'vue'
import {
  notificationOnboardingService,
  type NotificationQRChannel,
  type NotificationQRSession
} from '../services/notification-onboarding'

const POLL_INTERVAL_MS = 1500

type NotificationQROptions = {
  onApplied?: (session: NotificationQRSession) => Promise<void> | void
}

export function useNotificationQR(channel: NotificationQRChannel, options: NotificationQROptions = {}) {
  const session = ref<NotificationQRSession | null>(null)
  const loading = ref(false)
  const polling = ref(false)
  const error = ref('')
  let timer: number | null = null
  let generation = 0

  const active = computed(() => session.value?.status === 'wait' || session.value?.status === 'scaned')

  function clearTimer() {
    if (timer != null) {
      window.clearTimeout(timer)
      timer = null
    }
  }

  function schedule(currentGeneration: number) {
    clearTimer()
    if (!active.value || currentGeneration !== generation) return
    timer = window.setTimeout(() => void poll(currentGeneration), POLL_INTERVAL_MS)
  }

  async function poll(currentGeneration = generation) {
    const sessionID = session.value?.session_id
    if (!sessionID || currentGeneration !== generation) return
    polling.value = true
    const result = await notificationOnboardingService.status(channel, sessionID)
    polling.value = false
    if (currentGeneration !== generation) return
    if (!result.ok) {
      error.value = result.error.message
      return
    }
    session.value = result.data
    error.value = result.data.error || result.data.apply_warning || ''
    if (result.data.applied) {
      await options.onApplied?.(result.data)
    }
    schedule(currentGeneration)
  }

  async function start(payload: { base_url?: string } = {}) {
    await cancelCurrent(false)
    const currentGeneration = ++generation
    session.value = null
    loading.value = true
    error.value = ''
    const result = await notificationOnboardingService.start(channel, payload)
    loading.value = false
    if (currentGeneration !== generation) return
    if (!result.ok) {
      error.value = result.error.message
      return
    }
    session.value = result.data
    schedule(currentGeneration)
  }

  async function cancelCurrent(reset = true) {
    clearTimer()
    const sessionID = session.value?.session_id
    generation++
    if (reset) {
      session.value = null
      error.value = ''
    }
    if (!sessionID) return
    const result = await notificationOnboardingService.cancel(channel, sessionID)
    if (reset && !result.ok) error.value = result.error.message
  }

  onBeforeUnmount(() => {
    clearTimer()
    const sessionID = session.value?.session_id
    generation++
    if (sessionID && active.value) {
      void notificationOnboardingService.cancel(channel, sessionID)
    }
  })

  return { session, loading, polling, error, active, start, cancel: () => cancelCurrent(true) }
}
