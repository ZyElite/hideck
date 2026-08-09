package smsdelivery

import "time"

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
