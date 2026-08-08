# VoWiFi Runtime Capabilities

This file records what the reconstructed runtime executes on production paths.
It distinguishes real network behavior from compatibility APIs that cannot
perform their historical action with the arguments they expose.

## Implemented Network Paths

| Area | Runtime behavior |
| --- | --- |
| SWu | Establishes and tears down the IKE/IPsec tunnel through the selected ePDG. Authentication and transport errors are returned to the caller. |
| IMS registration | Sends REGISTER over the protected IMS transport, performs AKA, retains the route and security agreement, handles registration-event SUBSCRIBE/NOTIFY, refreshes before expiry, and reports terminal refresh errors. |
| SIP transport | Correlates responses by Call-ID, CSeq, method, and top Via branch. TCP and UDP receivers dispatch requests and close their transactions and sockets during shutdown. |
| SMS readiness | Becomes ready only when IMS registration, an inbound SIP receiver, and SMSC discovery are all ready. Losing any prerequisite clears readiness. |
| Outbound SMS | Encodes SMS-SUBMIT and RP-DATA, sends a real SIP MESSAGE transaction, accepts only a final 2xx response, and tracks RP/TP delivery outcomes when requested. |
| Inbound SMS | Validates SIP MESSAGE and 3GPP payloads, decodes RP-DATA and SMS-DELIVER, responds over the inbound SIP path, publishes the message event, and sends RP acknowledgement or error. |
| Multipart SMS | Splits outbound text, reassembles inbound parts by sender/reference, rejects conflicting duplicates, expires incomplete groups, and persists delivery updates. |
| USSI/USSD | Uses real INVITE, ACK, INFO, and BYE transactions and routes inbound INFO/BYE to the active session. |
| Voice signaling | Uses real outbound INVITE/ACK/BYE/CANCEL transactions, routes established-dialog requests, refreshes sessions with re-INVITE, and tears down call timers and runtime bindings. |

## Explicit Capability Boundaries

- A new inbound voice INVITE is rejected with `486 Busy Here`. The current
  public runtime has no local client request object with which to deliver and
  answer a new incoming call.
- An established-dialog re-INVITE carrying an SDP offer is rejected with
  `488 Not Acceptable Here` because the runtime cannot construct a negotiated
  media answer without a configured RTP bridge. Header-only refresh requests
  and UPDATE remain supported.
- Voice signaling is implemented, but the default SDP advertises a disabled
  audio port until an RTP relay is configured by a caller. This is not a claim
  of end-to-end audio readiness.
- Compatibility handles that contain only a Call-ID cannot answer an inbound
  request, address an in-dialog request, or retransmit PRACK. These methods
  return explicit context errors instead of reporting success.
- The legacy gateway packet-capture methods have no output target parameter.
  They return an explicit configuration error; per-call capture remains
  available when the caller supplies a writable output.
- The optional LAN-side voice bridge starts only after a caller injects a real
  packet connection and remote address. Queueing a packet is distinct from
  network delivery; asynchronous write failures remain available through the
  bridge error accessor.
- The legacy global dataplane cleanup function lacks an owning session or
  interface identifier and therefore returns an explicit error. Production
  cleanup is performed by the runtime-owned SWu session during shutdown.

No SMS, registration, USSI, or voice transaction reports network success
before receiving the corresponding final SIP response.
