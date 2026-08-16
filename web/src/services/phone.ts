import { api } from '../stores/auth'
import { readPhoneEvents } from './phone-events'

export type PhoneDevice = {
  id: string
  name: string
  iccid: string
  voice: { ready?: boolean; registered?: boolean; active_call?: boolean }
  phone_mode?: string      // "wifi" | "cellular"
  data_strategy?: string   // "always" | "on_demand"
}

export type PhoneCall = {
  call_id: string
  device_id: string
  direction: 'inbound' | 'outbound'
  peer: string
  status: string
  media_id?: string
  started_at: string
  answered_at?: string
  ended_at?: string
  end_reason?: string
  codec?: string
  recording_error?: string
  read_only: boolean
}

export type PhoneRecord = {
  id: number
  call_id: string
  device_id: string
  direction: 'inbound' | 'outbound'
  peer: string
  status: string
  started_at: string
  answered_at?: string
  ended_at?: string
  duration_seconds: number
  end_reason?: string
  codec?: string
  recording_name?: string
  recording_error?: string
}

export type PhoneEvent = {
  id: number
  type: string
  call: PhoneCall
  time: string
}

function leaseHeaders(lease: string) {
  return lease ? { 'X-Phone-Lease': lease } : undefined
}

export const phoneService = {
  async devices() {
    return (await api.get<{ devices: PhoneDevice[] }>('/phone/devices')).data.devices
  },

  async createMedia(sdp: string) {
    return (await api.post<{ media_id: string; lease: string; sdp: string }>('/phone/media', { sdp })).data
  },

  async active(lease: string) {
    return (await api.get<{ calls: PhoneCall[] }>('/phone/calls/active', { headers: leaseHeaders(lease) })).data.calls
  },

  async history(limit = 50) {
    return (await api.get<{ records: PhoneRecord[] }>('/phone/history', { params: { limit } })).data.records
  },

  async startCall(deviceId: string, callee: string, mediaId: string, lease: string) {
    return (await api.post<PhoneCall>('/phone/calls', {
      device_id: deviceId,
      callee,
      media_id: mediaId
    }, { headers: leaseHeaders(lease) })).data
  },

  async answer(callId: string, mediaId: string, lease: string) {
    return (await api.post<PhoneCall>(`/phone/calls/${encodeURIComponent(callId)}/answer`, {
      media_id: mediaId
    }, { headers: leaseHeaders(lease) })).data
  },

  async reject(callId: string, mediaId: string, lease: string) {
    await api.post(`/phone/calls/${encodeURIComponent(callId)}/reject`, {
      media_id: mediaId
    }, { headers: leaseHeaders(lease) })
  },

  async hangup(callId: string, lease: string) {
    await api.delete(`/phone/calls/${encodeURIComponent(callId)}`, { headers: leaseHeaders(lease) })
  },

  async dtmf(callId: string, digit: string, lease: string) {
    await api.post(`/phone/calls/${encodeURIComponent(callId)}/dtmf`, { digit }, { headers: leaseHeaders(lease) })
  },

  async refreshMedia(callId: string, mediaId: string, lease: string, takeover = false) {
    return (await api.put<{ call: PhoneCall; lease: string }>(
      `/phone/calls/${encodeURIComponent(callId)}/media`,
      { media_id: mediaId, takeover },
      { headers: leaseHeaders(lease) }
    )).data
  },

  async recording(name: string) {
    return (await api.get<Blob>(`/phone/recordings/${encodeURIComponent(name)}`, { responseType: 'blob' })).data
  },

  async events(
    afterId: number,
    signal: AbortSignal,
    onEvent: (event: PhoneEvent) => void,
    onOpen?: () => void
  ) {
    const token = localStorage.getItem('token') || ''
    const response = await fetch(`/api/phone/events?after_id=${afterId}`, {
      headers: {
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        'Last-Event-ID': String(afterId)
      },
      signal
    })
    if (!response.ok || !response.body) {
      throw new Error(`电话事件流连接失败（HTTP ${response.status}）`)
    }
    onOpen?.()
    await readPhoneEvents(response.body, onEvent)
  }
}
