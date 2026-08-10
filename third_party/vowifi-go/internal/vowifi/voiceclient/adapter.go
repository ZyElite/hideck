// Package voiceclient defines the local voice client integration contract.
package voiceclient

import "github.com/emiago/sipgo"

// Adapter is the v1.5.5 runtimehost-to-voice client contract.
type Adapter interface {
	GetClient() *sipgo.Client
	GetClientContact(deviceID string) (string, string, string, error)
	GetExternalIP() string
	GetListenAddr() string
	GetUA() *sipgo.UserAgent
	RTPPortRange() (int, int)
	SendPushNotification(title, body, category, callID string) error
	SubscribeDeviceOnline(deviceID string) <-chan struct{}
}
