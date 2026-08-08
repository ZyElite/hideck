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
| Voice signaling and media | Uses real outbound and inbound INVITE/ACK/BYE/CANCEL transactions. New inbound calls are exposed through the runtime gateway, ring with `180`, can be answered or rejected over the retained network transaction, renegotiate SDP, and relay RTP in both directions with payload-type mapping. The legacy timed call allocates a non-zero IMS RTP endpoint and transmits 20 ms PCMU media until BYE. Dialogs, timers, sockets, and runtime bindings are released on failure, cancel, or hangup. |

## Explicit Capability Boundaries

- Inbound voice consumers must provide a real client SDP answer through
  `voicehost.Gateway.AnswerIncomingCall`. The gateway also exposes callbacks
  and polling for pending calls. Reject, no-answer, and CANCEL paths send an
  explicit final response to the original INVITE.
- `Agent.Dial`, `Agent.DialContext`, and the legacy timed `SimulateCall` API
  preserve the old self-contained call mode: they allocate an RTP relay before
  sending INVITE, advertise the relay's non-zero IMS port, require PCMU in the
  network SDP answer, and send PCMU comfort media until hangup. Local client
  calls use `HandleClientInvite`, which injects the client's SDP and relays RTP
  in both directions.
- The client side of the RTP relay is advertised on `127.0.0.1`; the media
  client therefore runs on the same host as the runtime. IMS-side media binds
  to the registered IMS address and uses ephemeral non-zero ports.
- Compatibility handles that contain only a Call-ID cannot retransmit PRACK.
  They return an explicit context error instead of reporting success.
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
