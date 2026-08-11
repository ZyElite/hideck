package db

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/carrierquery"
	"gorm.io/gorm"
)

type CommandExecution struct {
	ID            string     `gorm:"primaryKey" json:"id"`
	Input         string     `gorm:"not null" json:"input"`
	Command       string     `gorm:"index;not null" json:"command"`
	ArgumentsJSON string     `gorm:"column:arguments_json;not null" json:"-"`
	State         string     `gorm:"index;not null" json:"state"`
	Error         string     `json:"error,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedAt     time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type CommandEvent struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ExecutionID string    `gorm:"index;not null" json:"execution_id"`
	Kind        string    `gorm:"not null" json:"kind"`
	Text        string    `gorm:"not null" json:"text"`
	PayloadJSON string    `gorm:"column:payload_json" json:"-"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

type BalanceQuery struct {
	ID            string     `gorm:"primaryKey" json:"id"`
	DeviceID      string     `gorm:"index;not null" json:"device_id"`
	ICCID         string     `gorm:"column:iccid;index:idx_balance_iccid_state,priority:1;not null" json:"iccid"`
	RuleID        string     `gorm:"index;not null" json:"rule_id"`
	Transport     string     `gorm:"not null" json:"transport"`
	State         string     `gorm:"index:idx_balance_iccid_state,priority:2;not null" json:"state"`
	ParseState    string     `gorm:"not null" json:"parse_state"`
	Amount        string     `json:"amount,omitempty"`
	Currency      string     `json:"currency,omitempty"`
	Summary       string     `json:"summary,omitempty"`
	RawResponse   string     `json:"raw_response,omitempty"`
	RequestSMSID  *uint      `json:"request_sms_id,omitempty"`
	ResponseSMSID *uint      `json:"response_sms_id,omitempty"`
	StartedAt     time.Time  `gorm:"index;not null" json:"started_at"`
	ExpiresAt     time.Time  `gorm:"index;not null" json:"expires_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Error         string     `json:"error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type CustomCarrierQueryRule struct {
	ID                  string    `gorm:"primaryKey" json:"id"`
	MCC                 string    `gorm:"index:idx_custom_carrier_match,priority:1;not null" json:"mcc"`
	MNC                 string    `gorm:"index:idx_custom_carrier_match,priority:2;not null" json:"mnc"`
	SPN                 string    `gorm:"index:idx_custom_carrier_match,priority:3" json:"spn,omitempty"`
	Variant             string    `json:"variant,omitempty"`
	Operator            string    `gorm:"not null" json:"operator"`
	Transport           string    `gorm:"not null" json:"transport"`
	Destination         string    `json:"destination,omitempty"`
	Payload             string    `json:"payload,omitempty"`
	ResponseMode        string    `gorm:"not null" json:"response_mode"`
	ExpectedSendersJSON string    `gorm:"column:expected_senders_json" json:"-"`
	ParserPattern       string    `json:"parser_pattern,omitempty"`
	Currency            string    `json:"currency,omitempty"`
	CostStatus          string    `json:"cost_status"`
	EvidenceType        string    `json:"evidence_type"`
	EvidenceURL         string    `json:"evidence_url,omitempty"`
	LimitationsJSON     string    `gorm:"column:limitations_json" json:"-"`
	Alternative         string    `json:"alternative,omitempty"`
	Enabled             bool      `json:"enabled"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func SaveCustomCarrierQueryRule(rule carrierquery.Rule) error {
	rule.BuiltIn = false
	if err := rule.Validate(); err != nil {
		return err
	}
	record, err := customRuleRecord(rule)
	if err != nil {
		return err
	}
	return DB.Save(&record).Error
}

func ListCustomCarrierQueryRules() ([]carrierquery.Rule, error) {
	var records []CustomCarrierQueryRule
	if err := DB.Order("mcc asc, mnc asc, id asc").Find(&records).Error; err != nil {
		return nil, err
	}
	return decodeCustomRules(records)
}

func GetCustomCarrierQueryRule(id string) (*carrierquery.Rule, error) {
	var record CustomCarrierQueryRule
	err := DB.First(&record, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rule, err := decodeCustomRule(record)
	return &rule, err
}

func DeleteCustomCarrierQueryRule(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("规则 ID 不能为空")
	}
	return DB.Delete(&CustomCarrierQueryRule{}, "id = ?", id).Error
}

func customRuleRecord(rule carrierquery.Rule) (CustomCarrierQueryRule, error) {
	senders, err := json.Marshal(rule.ExpectedSenders)
	if err != nil {
		return CustomCarrierQueryRule{}, err
	}
	limitations, err := json.Marshal(rule.Limitations)
	if err != nil {
		return CustomCarrierQueryRule{}, err
	}
	return CustomCarrierQueryRule{ID: rule.ID, MCC: rule.MCC, MNC: rule.MNC, SPN: rule.SPN, Variant: rule.Variant,
		Operator: rule.Operator, Transport: string(rule.Transport), Destination: rule.Destination, Payload: rule.Payload,
		ResponseMode: string(rule.ResponseMode), ExpectedSendersJSON: string(senders), ParserPattern: rule.ParserPattern,
		Currency: rule.Currency, CostStatus: rule.CostStatus, EvidenceType: rule.EvidenceType, EvidenceURL: rule.EvidenceURL,
		LimitationsJSON: string(limitations), Alternative: rule.Alternative, Enabled: rule.Enabled}, nil
}

func decodeCustomRules(records []CustomCarrierQueryRule) ([]carrierquery.Rule, error) {
	rules := make([]carrierquery.Rule, 0, len(records))
	for _, record := range records {
		rule, err := decodeCustomRule(record)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func decodeCustomRule(record CustomCarrierQueryRule) (carrierquery.Rule, error) {
	rule := carrierquery.Rule{ID: record.ID, MCC: record.MCC, MNC: record.MNC, SPN: record.SPN, Variant: record.Variant,
		Operator: record.Operator, Transport: carrierquery.Transport(record.Transport), Destination: record.Destination,
		Payload: record.Payload, ResponseMode: carrierquery.ResponseMode(record.ResponseMode), ParserPattern: record.ParserPattern,
		Currency: record.Currency, CostStatus: record.CostStatus, EvidenceType: record.EvidenceType, EvidenceURL: record.EvidenceURL,
		Alternative: record.Alternative, Enabled: record.Enabled, BuiltIn: false}
	if err := json.Unmarshal([]byte(record.ExpectedSendersJSON), &rule.ExpectedSenders); err != nil {
		return carrierquery.Rule{}, err
	}
	if err := json.Unmarshal([]byte(record.LimitationsJSON), &rule.Limitations); err != nil {
		return carrierquery.Rule{}, err
	}
	return rule, rule.Validate()
}
