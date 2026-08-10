package smsdelivery

import (
	"errors"
	"time"
)

// SendOutcome is returned after IMS accepts all SMS parts for delivery.
type SendOutcome struct {
	MessageID     string `json:"message_id"`
	PartsTotal    int    `json:"parts_total"`
	DeliveryState string `json:"delivery_state"`
}

type DeliveryPartMatch struct {
	MessageID string
	PartNo    int
	State     string
	// Matched is additive compatibility for stores that distinguish an empty
	// match from an unavailable record.
	Matched bool
}

type DeliveryPartStatus struct {
	PartNo      int        `json:"part_no"`
	CallID      string     `json:"call_id"`
	InReplyTo   string     `json:"in_reply_to"`
	RPMR        int        `json:"rp_mr"`
	State       string     `json:"state"`
	SIPCode     int        `json:"sip_code"`
	RPCause     int        `json:"rp_cause"`
	RPCauseText string     `json:"rp_cause_text,omitempty"`
	ErrorText   string     `json:"error_text"`
	SentAt      time.Time  `json:"sent_at"`
	ReportAt    *time.Time `json:"report_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type DeliveryStatus struct {
	MessageID  string               `json:"message_id"`
	IMSI       string               `json:"imsi"`
	DeviceID   string               `json:"device_id"`
	Peer       string               `json:"peer"`
	Content    string               `json:"content"`
	PartsTotal int                  `json:"parts_total"`
	Acks       int                  `json:"acks"`
	State      string               `json:"state"`
	LastError  string               `json:"last_error"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
	Parts      []DeliveryPartStatus `json:"parts"`
}

type Store interface {
	CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, at time.Time) error
	GetSMSDeliveryStatus(messageID string) (*DeliveryStatus, error)
	MarkSMSDeliveryPartReport(inReplyTo, callID, deviceID string, rpMR int, state string, sipCode, rpCause int, errText string, at time.Time) (DeliveryPartMatch, error)
	RecomputeSMSDelivery(messageID string, at time.Time) error
	UpdateSMSDeliveryState(messageID, state, lastError string, acks int, at time.Time) error
	UpsertSMSDeliveryPart(messageID string, partNo int, callID string, rpMR int, state string, sentAt time.Time) error
}

// SIPResultStore is the optional initial SIP transaction result capability.
type SIPResultStore interface {
	MarkSMSDeliveryPartSIPResult(
		messageID string,
		partNo, sipCode int,
		state, errText string,
		at time.Time,
	) error
}

// ErrInboundFragmentCollision reports a reused multipart identity whose
// sequence metadata or content differs from the stored fragment.
var ErrInboundFragmentCollision = errors.New("SMS inbound fragment collision")

// InboundFragmentOwner isolates persisted fragments between subscriptions.
type InboundFragmentOwner struct {
	DeviceID string
	IMSI     string
}

// InboundFragmentScope identifies one multipart message for an owner.
type InboundFragmentScope struct {
	Owner      InboundFragmentOwner
	SessionKey string
}

// InboundFragment is the restart-safe form of an incomplete MT fragment.
type InboundFragment struct {
	Reference     int
	ReferenceBits int
	Total         int
	Sequence      int
	Content       string
	ArrivedAt     time.Time
	RPMR          int
	CallID        string
	ToURI         string
	ServiceCenter string
	AckSent       bool
	AckSentAt     time.Time
	DegradedAt    time.Time
}

// StoredInboundFragment associates a fragment with its persisted session.
type StoredInboundFragment struct {
	Scope    InboundFragmentScope
	Fragment InboundFragment
}

// InboundFragmentSaveResult is an atomic snapshot after one fragment save.
type InboundFragmentSaveResult struct {
	Inserted        bool
	CollisionReason string
	Fragments       []InboundFragment
}

// InboundFragmentStore is an optional delivery-store capability. Implementers
// must save and return the session snapshot atomically.
type InboundFragmentStore interface {
	LoadInboundFragments(owner InboundFragmentOwner) ([]StoredInboundFragment, error)
	SaveInboundFragment(scope InboundFragmentScope, fragment InboundFragment) (InboundFragmentSaveResult, error)
	DeleteInboundFragments(scope InboundFragmentScope) error
	MarkInboundFragmentAcked(scope InboundFragmentScope, sequence int, at time.Time) error
}

// InboundFragmentLifecycleStore is an optional extension used when a durable
// store can remember that an incomplete message was already published.
type InboundFragmentLifecycleStore interface {
	MarkInboundFragmentsDegraded(scope InboundFragmentScope, at time.Time) error
}
