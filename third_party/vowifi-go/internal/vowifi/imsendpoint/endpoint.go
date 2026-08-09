package imsendpoint

// Endpoint is the runtime-owned IMS service surface needed by voice lifecycle binding.
// The full call and dialog contract is restored with imscore and voice.
type Endpoint interface {
	DeviceID() string
	IsRegistered() bool
}
