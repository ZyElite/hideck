package smsdelivery

import "time"

type DeliveryPartMatch struct {
	MessageID string
	PartNo    int
	State     string
}

type DeliveryPartStatus struct {
	PartNo      int        `json:"part_no"`
	CallID      string     `json:"call_id"`
	InReplyTo   string     `json:"in_reply_to"`
	RPMR        int        `json:"rp_mr"`
	State       string     `json:"state"`
	SIPCode     int        `json:"sip_code"`
	RPCause     int        `json:"rp_cause"`
	RPCauseText string     `json:"rp_cause_text"`
	ErrorText   string     `json:"error_text"`
	SentAt      time.Time  `json:"sent_at"`
	ReportAt    *time.Time `json:"report_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type DeliveryStatus struct {
	MessageID  string
	IMSI       string
	DeviceID   string
	Peer       string
	Content    string
	PartsTotal int
	Acks       int
	State      string
	LastError  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Parts      []DeliveryPartStatus
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
