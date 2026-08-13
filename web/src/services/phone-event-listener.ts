import { phoneService, type PhoneEvent } from './phone'

const EVENT_RETRY_MS = 2_000

type ListenerCallbacks = {
  cursor: () => number
  onEvent: (event: PhoneEvent) => void
  onError: (message: string) => void
  onOpen: () => void
}

export class PhoneEventListener {
  private abort: AbortController | null = null
  private listening = false

  constructor(private readonly callbacks: ListenerCallbacks) {}

  start() {
    if (this.listening) return
    this.listening = true
    void this.listen()
  }

  stop() {
    this.listening = false
    this.abort?.abort()
    this.abort = null
  }

  private async listen() {
    while (this.listening) {
      this.abort = new AbortController()
      try {
        await phoneService.events(
          this.callbacks.cursor(),
          this.abort.signal,
          this.callbacks.onEvent,
          this.callbacks.onOpen
        )
        if (this.listening) throw new Error('电话事件流已关闭')
      } catch (error) {
        if (!this.listening || isAbortError(error)) return
        this.callbacks.onError(errorMessage(error, '电话事件流连接失败'))
        await retryDelay(this.abort.signal)
      }
    }
  }
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback
}

function isAbortError(error: unknown) {
  return typeof DOMException !== 'undefined' && error instanceof DOMException && error.name === 'AbortError'
}

function retryDelay(signal: AbortSignal) {
  return new Promise<void>((resolve) => {
    const timer = window.setTimeout(resolve, EVENT_RETRY_MS)
    signal.addEventListener('abort', () => {
      window.clearTimeout(timer)
      resolve()
    }, { once: true })
  })
}
