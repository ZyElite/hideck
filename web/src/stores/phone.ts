import { defineStore } from 'pinia'
import { phoneService, type PhoneCall, type PhoneDevice, type PhoneEvent, type PhoneRecord } from '../services/phone'
import { PhoneEventListener } from '../services/phone-event-listener'
import { PhoneMediaController, type PhoneMediaState } from '../services/phone-media'
import {
  readPhoneControl,
  savePhoneControl,
  type SavedPhoneControl
} from '../services/phone-session'
import { normalizeCallOwnership, phoneErrorMessage, shouldRefreshCallMedia } from '../utils/phone'

const ACTIVE_STATUSES = new Set(['calling', 'ringing', 'connected'])
export type PhoneMediaMode = 'none' | 'listen-only' | 'two-way'

type PhoneState = {
  initialized: boolean
  loading: boolean
  devices: PhoneDevice[]
  calls: PhoneCall[]
  history: PhoneRecord[]
  mediaId: string
  lease: string
  mediaState: PhoneMediaState
  mediaMode: PhoneMediaMode
  muted: boolean
  error: string
  eventError: string
  listening: boolean
  lastEventId: number
  now: number
}

let clockTimer: number | null = null
let mediaController: PhoneMediaController | null = null
let eventListener: PhoneEventListener | null = null

export const usePhoneStore = defineStore('phone', {
  state: (): PhoneState => ({
    initialized: false,
    loading: false,
    devices: [],
    calls: [],
    history: [],
    mediaId: '',
    lease: '',
    mediaState: 'idle',
    mediaMode: 'none',
    muted: false,
    error: '',
    eventError: '',
    listening: false,
    lastEventId: 0,
    now: Date.now()
  }),
  getters: {
    currentCall(state): PhoneCall | undefined {
      return state.calls.find((call) => call.media_id === state.mediaId)
        || state.calls.find((call) => !call.read_only)
        || state.calls.find((call) => call.status === 'ringing')
        || state.calls[0]
    },
    mediaReady(state) {
      return state.mediaState === 'connecting' || state.mediaState === 'connected'
    },
    secureContext() {
      return typeof window !== 'undefined' && window.location.protocol === 'https:' && window.isSecureContext
    }
  },
  actions: {
    async initialize() {
      if (this.initialized) return
      this.initialized = true
      this.loading = true
      this.restoreControl()
      this.ensureMediaController()
      try {
        const [devices, calls, history] = await Promise.all([
          phoneService.devices(),
          phoneService.active(this.lease),
          phoneService.history()
        ])
        this.devices = devices
        this.calls = calls
        this.history = history
      } catch (error) {
        this.error = phoneErrorMessage(error, '电话服务加载失败')
      } finally {
        this.loading = false
      }
      this.startEventStream()
      this.startClock()
    },

    dispose() {
      this.listening = false
      eventListener?.stop()
      eventListener = null
      if (clockTimer !== null) window.clearInterval(clockTimer)
      clockTimer = null
      mediaController?.close()
      mediaController = null
      this.mediaState = 'idle'
      this.mediaMode = 'none'
      this.initialized = false
    },

    async refresh() {
      const [devices, calls, history] = await Promise.all([
        phoneService.devices(), phoneService.active(this.lease), phoneService.history()
      ])
      this.devices = devices
      this.calls = calls
      this.history = history
    },

    async enableMedia() {
      return this.refreshCurrentMedia('two-way')
    },

    async enableListenOnlyMedia() {
      return this.refreshCurrentMedia('listen-only')
    },

    async refreshCurrentMedia(mode: Exclude<PhoneMediaMode, 'none'>) {
      const current = this.currentCall
      const previous: SavedPhoneControl = { mediaId: this.mediaId, lease: this.lease }
      const prepared = mode === 'two-way'
        ? await this.prepareMedia()
        : await this.prepareReceiveOnlyMedia()
      if (!current || !shouldRefreshCallMedia(current, prepared.mediaId)) return
      try {
        const result = await phoneService.refreshMedia(current.call_id, prepared.mediaId, previous.lease)
        this.lease = result.lease
        this.upsertCall(result.call)
        this.saveControl()
      } catch (error) {
        this.restoreSavedControl(previous)
        throw error
      }
    },

    async takeOver(call: PhoneCall) {
      const prepared = await this.prepareMedia()
      try {
        const result = await phoneService.refreshMedia(call.call_id, prepared.mediaId, '', true)
        this.lease = result.lease
        this.upsertCall(result.call)
        this.saveControl()
      } catch (error) {
        this.releaseMedia()
        throw error
      }
    },

    async startCall(deviceId: string, callee: string) {
      return this.startCallWithMode(deviceId, callee, 'two-way')
    },

    async startListenOnlyCall(deviceId: string, callee: string) {
      return this.startCallWithMode(deviceId, callee, 'listen-only')
    },

    async startCallWithMode(
      deviceId: string,
      callee: string,
      mode: Exclude<PhoneMediaMode, 'none'>
    ) {
      this.error = ''
      if (this.calls.some((call) => ACTIVE_STATUSES.has(call.status) && !call.read_only)) {
        throw new Error('当前浏览器已有一通活动电话')
      }
      const media = mode === 'two-way'
        ? await this.prepareMedia()
        : await this.prepareReceiveOnlyMedia()
      try {
        const call = await phoneService.startCall(deviceId, callee, media.mediaId, media.lease)
        this.upsertCall(call)
        return call
      } catch (error) {
        this.releaseMedia()
        throw error
      }
    },

    async answer(call: PhoneCall) {
      const media = await this.prepareMedia()
      try {
        const answered = await phoneService.answer(call.call_id, media.mediaId, media.lease)
        this.upsertCall(answered)
      } catch (error) {
        this.releaseMedia()
        throw error
      }
    },

    async answerListenOnly(call: PhoneCall) {
      const media = await this.prepareReceiveOnlyMedia()
      try {
        const answered = await phoneService.answer(call.call_id, media.mediaId, media.lease)
        this.upsertCall(answered)
      } catch (error) {
        this.releaseMedia()
        throw error
      }
    },

    async reject(call: PhoneCall) {
      const media = await this.prepareControlMedia()
      await phoneService.reject(call.call_id, media.mediaId, media.lease)
      this.releaseMedia()
    },

    async hangup(call?: PhoneCall) {
      const target = call || this.currentCall
      if (!target) return
      await phoneService.hangup(target.call_id, this.lease)
    },

    async sendDTMF(digit: string) {
      const call = this.currentCall
      if (!call) throw new Error('当前没有活动电话')
      await phoneService.dtmf(call.call_id, digit, this.lease)
    },

    toggleMute() {
      if (this.mediaMode !== 'two-way') return
      this.muted = !this.muted
      mediaController?.setMuted(this.muted)
    },

    clearError() {
      this.error = ''
    },

    async recordingURL(name: string) {
      const blob = await phoneService.recording(name)
      return URL.createObjectURL(blob)
    },

    ensureMediaController() {
      if (mediaController) return
      mediaController = new PhoneMediaController({
        onState: (state) => { this.mediaState = state },
        onError: (message) => { this.error = message }
      })
    },

    hasReusableMedia(mode: Exclude<PhoneMediaMode, 'none'>) {
      return this.mediaReady && !!this.mediaId && !!this.lease && this.mediaMode === mode
    },

    async prepareMedia() {
      if (this.hasReusableMedia('two-way')) {
        return { mediaId: this.mediaId, lease: this.lease }
      }
      this.ensureMediaController()
      this.error = ''
      try {
        const prepared = await mediaController!.prepare()
        this.mediaId = prepared.mediaId
        this.lease = prepared.lease
        this.mediaMode = 'two-way'
        this.muted = false
        this.saveControl()
        return prepared
      } catch (error) {
        this.error = phoneErrorMessage(error, '听筒和麦克风启用失败')
        throw error
      }
    },

    async prepareControlMedia() {
      return this.prepareReceiveOnlyMedia()
    },

    async prepareReceiveOnlyMedia() {
      if (this.hasReusableMedia('listen-only')) {
        return { mediaId: this.mediaId, lease: this.lease }
      }
      this.ensureMediaController()
      const prepared = await mediaController!.prepare({ microphone: false })
      this.mediaId = prepared.mediaId
      this.lease = prepared.lease
      this.mediaMode = 'listen-only'
      this.saveControl()
      return prepared
    },

    upsertCall(call: PhoneCall) {
      const normalized = normalizeCallOwnership(call, this.mediaId)
      const index = this.calls.findIndex((item) => item.call_id === call.call_id)
      if (index >= 0) this.calls = this.calls.map((item, position) => position === index ? normalized : item)
      else this.calls = [normalized, ...this.calls]
    },

    handleEvent(event: PhoneEvent) {
      if (event.id <= this.lastEventId) return
      this.lastEventId = event.id
      if (ACTIVE_STATUSES.has(event.call.status)) this.upsertCall(event.call)
      else this.calls = this.calls.filter((call) => call.call_id !== event.call.call_id)
      if (event.type === 'call_ended') {
        if (event.call.media_id === this.mediaId) this.releaseMedia()
        void this.reloadHistory()
      } else if (event.type === 'recording_ready' || event.type === 'recording_failed') {
        void this.reloadHistory()
      } else if (event.type === 'media_disconnected' && event.call.media_id === this.mediaId) {
        this.error = '浏览器媒体连接已断开，请在 15 秒内恢复听筒，否则电话将自动挂断'
      }
    },

    startEventStream() {
      if (this.listening) return
      this.listening = true
      eventListener = new PhoneEventListener({
        cursor: () => this.lastEventId,
        onEvent: (event) => this.handleEvent(event),
        onError: (message) => { this.eventError = message },
        onOpen: () => { this.eventError = '' }
      })
      eventListener.start()
    },

    async reloadHistory() {
      try {
        this.history = await phoneService.history()
      } catch (error) {
        this.error = phoneErrorMessage(error, '最近通话加载失败')
      }
    },

    startClock() {
      if (clockTimer !== null) return
      clockTimer = window.setInterval(() => { this.now = Date.now() }, 1_000)
    },

    restoreControl() {
      const saved = readPhoneControl()
      this.mediaId = saved.mediaId
      this.lease = saved.lease
    },

    saveControl() {
      savePhoneControl({ mediaId: this.mediaId, lease: this.lease })
    },

    restoreSavedControl(control: SavedPhoneControl) {
      mediaController?.close()
      mediaController = null
      this.mediaId = control.mediaId
      this.lease = control.lease
      this.mediaState = 'idle'
      this.mediaMode = 'none'
      this.muted = false
      this.saveControl()
    },

    releaseMedia() {
      mediaController?.close()
      mediaController = null
      this.mediaId = ''
      this.lease = ''
      this.mediaState = 'idle'
      this.mediaMode = 'none'
      this.muted = false
      savePhoneControl({ mediaId: '', lease: '' })
    }
  }
})
