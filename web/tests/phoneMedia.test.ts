import assert from 'node:assert/strict'
import test from 'node:test'
import {
  PhoneMediaController,
  isTrustedHTTPSContext,
  supportsPCMU,
  type PhoneMediaDependencies,
  type PhoneMediaState
} from '../src/services/phone-media'

const PCMU_OFFER = 'v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=rtpmap:0 PCMU/8000\r\n'
const RECVONLY_PCMU_OFFER = `${PCMU_OFFER}a=recvonly\r\n`

type FakeTrack = MediaStreamTrack & { stopped: boolean }

function mediaFixture(secure = true) {
  const states: PhoneMediaState[] = []
  const track = {
    enabled: true,
    stopped: false,
    stop() { this.stopped = true }
  } as FakeTrack
  const microphone = {
    active: true,
    getAudioTracks: () => [track],
    getTracks: () => [track]
  } as unknown as MediaStream
  const peers: FakePeer[] = []
  let requestedConstraints: MediaStreamConstraints | null = null
  const requestedOffers: string[] = []
  const microphoneStoppedAtMediaCreation: boolean[] = []
  const dependencies: PhoneMediaDependencies = {
    secureContext: () => secure,
    getUserMedia: async (constraints) => {
      requestedConstraints = constraints
      return microphone
    },
    createPeer: () => {
      const peer = new FakePeer()
      peers.push(peer)
      return peer as unknown as RTCPeerConnection
    },
    createAudio: () => ({ autoplay: false, srcObject: null, play: async () => {} }) as unknown as HTMLAudioElement,
    createMedia: async (sdp) => {
      requestedOffers.push(sdp)
      microphoneStoppedAtMediaCreation.push(track.stopped)
      const suffix = requestedOffers.length
      return { media_id: `media-${suffix}`, lease: `lease-${suffix}`, sdp: PCMU_OFFER }
    },
    setTimer: () => 1,
    clearTimer: () => {}
  }
  const controller = new PhoneMediaController({
    onState: (state) => states.push(state),
    onError: (message) => assert.fail(message)
  }, dependencies)
  return {
    controller,
    states,
    track,
    peers,
    constraints: () => requestedConstraints,
    offers: () => requestedOffers,
    microphoneStoppedAtMediaCreation: () => microphoneStoppedAtMediaCreation
  }
}

test('refuses microphone access outside a trusted secure context', async () => {
  const fixture = mediaFixture(false)
  await assert.rejects(fixture.controller.prepare(), /HTTPS 安全上下文/)
  assert.equal(fixture.constraints(), null)
})

test('creates a receive-only listening session without microphone access', async () => {
  const fixture = mediaFixture(false)
  assert.deepEqual(
    await fixture.controller.prepare({ microphone: false }),
    { mediaId: 'media-1', lease: 'lease-1' }
  )
  assert.equal(fixture.constraints(), null)
  assert.equal(fixture.peers[0].transceiverDirection, 'recvonly')
  assert.deepEqual(fixture.offers(), [RECVONLY_PCMU_OFFER])
  fixture.controller.close()
})

test('switching from two-way to receive-only stops microphone capture before replacement', async () => {
  const fixture = mediaFixture()
  await fixture.controller.prepare()
  assert.equal(fixture.track.stopped, false)
  await fixture.controller.prepare({ microphone: false })
  assert.equal(fixture.track.stopped, true)
  assert.equal(fixture.peers[0].closed, true)
  assert.equal(fixture.peers[1].transceiverDirection, 'recvonly')
  assert.deepEqual(fixture.offers(), [PCMU_OFFER, RECVONLY_PCMU_OFFER])
  assert.deepEqual(fixture.microphoneStoppedAtMediaCreation(), [false, true])
  fixture.controller.close()
})

test('prepares PCMU media, controls mute, and releases browser resources', async () => {
  const fixture = mediaFixture()
  assert.deepEqual(await fixture.controller.prepare(), { mediaId: 'media-1', lease: 'lease-1' })
  assert.deepEqual(fixture.states, ['requesting', 'connecting'])
  assert.equal((fixture.constraints()?.audio as MediaTrackConstraints).channelCount, 1)
  assert.deepEqual(fixture.offers(), [PCMU_OFFER])
  assert.equal(fixture.peers[0].remoteDescription?.sdp, PCMU_OFFER)
  fixture.controller.setMuted(true)
  assert.equal(fixture.track.enabled, false)
  fixture.controller.close()
  assert.equal(fixture.track.stopped, true)
  assert.equal(fixture.peers[0].closed, true)
  assert.equal(fixture.states.at(-1), 'idle')
})

test('recognizes only an explicit PCMU 8000 SDP mapping', () => {
  assert.equal(supportsPCMU(PCMU_OFFER), true)
  assert.equal(supportsPCMU('a=rtpmap:8 PCMA/8000\r\n'), false)
})

test('does not treat the localhost HTTP exception as an HTTPS phone context', () => {
  assert.equal(isTrustedHTTPSContext('http:', true, true), false)
  assert.equal(isTrustedHTTPSContext('https:', false, true), false)
  assert.equal(isTrustedHTTPSContext('https:', true, true), true)
})

class FakePeer extends EventTarget {
  iceGatheringState: RTCIceGatheringState = 'complete'
  connectionState: RTCPeerConnectionState = 'new'
  localDescription: RTCSessionDescription | null = null
  remoteDescription: RTCSessionDescription | null = null
  closed = false
  transceiverDirection = ''
  private readonly transceiver = { sender: {}, setCodecPreferences: () => {} } as unknown as RTCRtpTransceiver

  addTrack() { return this.transceiver.sender }
  addTransceiver(_track: string, init?: RTCRtpTransceiverInit) {
    this.transceiverDirection = init?.direction || ''
    return this.transceiver
  }
  getTransceivers() { return [this.transceiver] }
  async createOffer() {
    const sdp = this.transceiverDirection === 'recvonly' ? RECVONLY_PCMU_OFFER : PCMU_OFFER
    return { type: 'offer', sdp } as RTCSessionDescriptionInit
  }
  async setLocalDescription(description: RTCLocalSessionDescriptionInit) {
    this.localDescription = description as RTCSessionDescription
  }
  async setRemoteDescription(description: RTCSessionDescriptionInit) {
    this.remoteDescription = description as RTCSessionDescription
  }
  close() { this.closed = true }
}
