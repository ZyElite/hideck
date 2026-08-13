import { phoneService } from './phone'

export type PhoneMediaState = 'idle' | 'requesting' | 'connecting' | 'connected' | 'disconnected' | 'failed'

type MediaCallbacks = {
  onState: (state: PhoneMediaState) => void
  onError: (message: string) => void
}

export type PhoneMediaDependencies = {
  secureContext: () => boolean
  getUserMedia: (constraints: MediaStreamConstraints) => Promise<MediaStream>
  createPeer: () => RTCPeerConnection
  createAudio: () => HTMLAudioElement
  createMedia: typeof phoneService.createMedia
  setTimer: (handler: () => void, timeout: number) => number
  clearTimer: (timer: number) => void
}

export type PreparedPhoneMedia = {
  mediaId: string
  lease: string
}

type PrepareMediaOptions = { microphone?: boolean }

const ICE_GATHERING_TIMEOUT_MS = 10_000

export class PhoneMediaController {
  private microphone: MediaStream | null = null
  private peer: RTCPeerConnection | null = null
  private readonly audio: HTMLAudioElement

  constructor(
    private readonly callbacks: MediaCallbacks,
    private readonly dependencies: PhoneMediaDependencies = browserMediaDependencies()
  ) {
    this.audio = dependencies.createAudio()
    this.audio.autoplay = true
  }

  async prepare(options: PrepareMediaOptions = {}): Promise<PreparedPhoneMedia> {
    const useMicrophone = options.microphone !== false
    if (useMicrophone) this.assertSecureContext()
    else this.releaseMicrophone()
    this.callbacks.onState('requesting')
    let peer: RTCPeerConnection | null = null
    try {
      const microphone = useMicrophone ? await this.getMicrophone() : null
      peer = this.createPeer(microphone)
      this.replacePeer(peer)
      const offer = await this.createOffer(peer)
      this.callbacks.onState('connecting')
      const answer = await this.dependencies.createMedia(offer)
      await peer.setRemoteDescription({ type: 'answer', sdp: answer.sdp })
      return { mediaId: answer.media_id, lease: answer.lease }
    } catch (error) {
      this.releaseFailedMedia(peer)
      this.callbacks.onState('failed')
      throw error
    }
  }

  setMuted(muted: boolean) {
    this.microphone?.getAudioTracks().forEach((track) => {
      track.enabled = !muted
    })
  }

  close() {
    this.peer?.close()
    this.peer = null
    this.releaseMicrophone()
    this.audio.srcObject = null
    this.callbacks.onState('idle')
  }

  private assertSecureContext() {
    if (!this.dependencies.secureContext()) {
      throw new Error('浏览器未处于受信任的 HTTPS 安全上下文，无法启用麦克风')
    }
  }

  private async getMicrophone() {
    if (this.microphone?.active) return this.microphone
    this.microphone = await this.dependencies.getUserMedia({
      audio: {
        channelCount: 1,
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true
      },
      video: false
    })
    return this.microphone
  }

  private createPeer(microphone: MediaStream | null) {
    const peer = this.dependencies.createPeer()
    try {
      const transceiver = microphone
        ? this.addMicrophone(peer, microphone)
        : peer.addTransceiver('audio', { direction: 'recvonly' })
      this.preferPCMU(transceiver)
      peer.addEventListener('track', (event) => this.attachRemoteAudio(event))
      peer.addEventListener('connectionstatechange', () => this.reportPeerState(peer))
      return peer
    } catch (error) {
      peer.close()
      throw error
    }
  }

  private addMicrophone(peer: RTCPeerConnection, microphone: MediaStream) {
    const track = microphone.getAudioTracks()[0]
    if (!track) throw new Error('浏览器没有返回可用的麦克风音轨')
    const sender = peer.addTrack(track, microphone)
    const transceiver = peer.getTransceivers().find((item) => item.sender === sender)
    if (!transceiver) throw new Error('浏览器未创建麦克风 WebRTC transceiver')
    return transceiver
  }

  private preferPCMU(transceiver: RTCRtpTransceiver) {
    const capabilities = typeof RTCRtpSender === 'undefined' ? null : RTCRtpSender.getCapabilities?.('audio')
    if (!transceiver.setCodecPreferences) return
    const codecs = selectPCMUCodecs(capabilities)
    if (codecs.length > 0) transceiver.setCodecPreferences(codecs)
  }

  private async createOffer(peer: RTCPeerConnection) {
    await peer.setLocalDescription(await peer.createOffer())
    await waitForICEGathering(peer, this.dependencies)
    const sdp = peer.localDescription?.sdp
    if (!sdp) throw new Error('浏览器未生成 WebRTC SDP offer')
    if (!supportsPCMU(sdp)) throw new Error('当前浏览器未提供 PCMU/8000 音频能力')
    return sdp
  }

  private replacePeer(peer: RTCPeerConnection) {
    const previous = this.peer
    this.peer = peer
    previous?.close()
  }

  private attachRemoteAudio(event: RTCTrackEvent) {
    const stream = event.streams[0] || new MediaStream([event.track])
    this.audio.srcObject = stream
    void this.audio.play().catch((error) => {
      this.callbacks.onError(toMediaError(error, '浏览器阻止了远端音频播放'))
    })
  }

  private reportPeerState(peer: RTCPeerConnection) {
    if (this.peer !== peer) return
    const state = peer.connectionState
    if (state === 'connected') this.callbacks.onState('connected')
    else if (state === 'disconnected' || state === 'closed') this.callbacks.onState('disconnected')
    else if (state === 'failed') this.callbacks.onState('failed')
  }

  private releaseFailedMedia(peer: RTCPeerConnection | null) {
    peer?.close()
    if (this.peer === peer) this.peer = null
    this.releaseMicrophone()
    this.audio.srcObject = null
  }

  private releaseMicrophone() {
    this.microphone?.getTracks().forEach((track) => track.stop())
    this.microphone = null
  }
}

export function selectPCMUCodecs(capabilities: RTCRtpCapabilities | null | undefined) {
  return capabilities?.codecs.filter((codec) => codec.mimeType.toLowerCase() === 'audio/pcmu') || []
}

export function supportsPCMU(sdp: string) {
  return /^a=rtpmap:\d+ PCMU\/8000(?:\/1)?\s*$/im.test(sdp)
}

export function isTrustedHTTPSContext(protocol: string, secure: boolean, mediaDevicesAvailable: boolean) {
  return protocol === 'https:' && secure && mediaDevicesAvailable
}

function waitForICEGathering(peer: RTCPeerConnection, dependencies: PhoneMediaDependencies) {
  if (peer.iceGatheringState === 'complete') return Promise.resolve()
  return new Promise<void>((resolve, reject) => {
    const timeout = dependencies.setTimer(
      () => finish(new Error('WebRTC ICE 候选收集超时')),
      ICE_GATHERING_TIMEOUT_MS
    )
    const onChange = () => {
      if (peer.iceGatheringState === 'complete') finish()
    }
    const finish = (error?: Error) => {
      dependencies.clearTimer(timeout)
      peer.removeEventListener('icegatheringstatechange', onChange)
      if (error) reject(error)
      else resolve()
    }
    peer.addEventListener('icegatheringstatechange', onChange)
  })
}

function browserMediaDependencies(): PhoneMediaDependencies {
  return {
    secureContext: () => isTrustedHTTPSContext(
      window.location.protocol,
      window.isSecureContext,
      typeof navigator.mediaDevices?.getUserMedia === 'function'
    ),
    getUserMedia: (constraints) => navigator.mediaDevices.getUserMedia(constraints),
    createPeer: () => new RTCPeerConnection(),
    createAudio: () => new Audio(),
    createMedia: (sdp) => phoneService.createMedia(sdp),
    setTimer: (handler, timeout) => window.setTimeout(handler, timeout),
    clearTimer: (timer) => window.clearTimeout(timer)
  }
}

function toMediaError(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? `${fallback}：${error.message}` : fallback
}
