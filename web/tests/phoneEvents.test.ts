import assert from 'node:assert/strict'
import test from 'node:test'
import { parsePhoneEventBlock, readPhoneEvents } from '../src/services/phone-events'
import { phoneService, type PhoneCall, type PhoneEvent } from '../src/services/phone'

function phoneCall(callId: string): PhoneCall {
  return {
    call_id: callId,
    device_id: 'wwan1',
    direction: 'inbound',
    peer: '10086',
    status: 'ringing',
    started_at: '2026-08-13T12:00:00Z',
    read_only: false
  }
}

function phoneEvent(id: number): PhoneEvent {
  return { id, type: 'incoming_call', call: phoneCall(`call-${id}`), time: '2026-08-13T12:00:00Z' }
}

test('parses SSE data while ignoring heartbeat and metadata fields', () => {
  const events: PhoneEvent[] = []
  parsePhoneEventBlock(': heartbeat', (event) => events.push(event))
  parsePhoneEventBlock(`id: 3\nevent: incoming_call\ndata: ${JSON.stringify(phoneEvent(3))}`, (event) => events.push(event))
  assert.deepEqual(events.map((event) => event.id), [3])
})

test('reads fragmented CRLF SSE blocks and flushes the final event', async () => {
  const encoder = new TextEncoder()
  const payload = [
    `id: 1\r\ndata: ${JSON.stringify(phoneEvent(1))}\r\n\r\n`,
    `id: 2\ndata: ${JSON.stringify(phoneEvent(2))}`
  ]
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode(payload[0].slice(0, 17)))
      controller.enqueue(encoder.encode(payload[0].slice(17) + payload[1]))
      controller.close()
    }
  })
  const events: PhoneEvent[] = []
  await readPhoneEvents(stream, (event) => events.push(event))
  assert.deepEqual(events.map((event) => event.id), [1, 2])
})

test('reconnect request carries the last applied event ID and authentication', async (context) => {
  let request: { url: string; init?: RequestInit } | null = null
  const originalFetch = globalThis.fetch
  Object.defineProperty(globalThis, 'localStorage', {
    value: { getItem: () => 'session-token' },
    configurable: true
  })
  globalThis.fetch = async (url, init) => {
    request = { url: String(url), init }
    return new Response(new ReadableStream({ start: (controller) => controller.close() }), { status: 200 })
  }
  context.after(() => {
    globalThis.fetch = originalFetch
    delete (globalThis as { localStorage?: unknown }).localStorage
  })

  await phoneService.events(37, new AbortController().signal, () => {})
  assert.equal(request?.url, '/api/phone/events?after_id=37')
  assert.deepEqual(request?.init?.headers, {
    Authorization: 'Bearer session-token',
    'Last-Event-ID': '37'
  })
})
