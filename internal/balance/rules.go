package balance

import (
	"context"
	"sort"
	"strings"

	"github.com/yibaiba/hideck/internal/carrierquery"
)

type CustomRuleSource interface {
	ListCustomCarrierQueryRules() ([]carrierquery.Rule, error)
}

type Rules struct {
	custom CustomRuleSource
}

func NewRules(custom CustomRuleSource) *Rules {
	return &Rules{custom: custom}
}

func (r *Rules) Resolve(_ context.Context, snapshot DeviceSnapshot) (carrierquery.Rule, error) {
	custom, err := r.customRules()
	if err != nil {
		return carrierquery.Rule{}, err
	}
	if rule, ok := bestCustomRule(custom, snapshot); ok {
		return rule, nil
	}
	rule, ok := carrierquery.FindBuiltIn(snapshot.MCC, snapshot.MNC)
	if !ok || !rule.Enabled || hasRuleID(custom, rule.ID) {
		return carrierquery.Rule{}, ErrRuleNotFound
	}
	return rule, nil
}

func (r *Rules) ByID(_ context.Context, id string) (carrierquery.Rule, error) {
	custom, err := r.customRules()
	if err != nil {
		return carrierquery.Rule{}, err
	}
	if rule, ok := findRuleByID(custom, id); ok {
		if !rule.Enabled {
			return carrierquery.Rule{}, ErrRuleNotFound
		}
		return rule, nil
	}
	for _, rule := range carrierquery.BuiltInRules() {
		if rule.ID == id {
			return rule, nil
		}
	}
	return carrierquery.Rule{}, ErrRuleNotFound
}

func findRuleByID(rules []carrierquery.Rule, id string) (carrierquery.Rule, bool) {
	id = strings.TrimSpace(id)
	for _, rule := range rules {
		if rule.ID == id {
			return rule, true
		}
	}
	return carrierquery.Rule{}, false
}

func hasRuleID(rules []carrierquery.Rule, id string) bool {
	_, found := findRuleByID(rules, id)
	return found
}

func (r *Rules) customRules() ([]carrierquery.Rule, error) {
	if r == nil || r.custom == nil {
		return nil, nil
	}
	return r.custom.ListCustomCarrierQueryRules()
}

func bestCustomRule(rules []carrierquery.Rule, snapshot DeviceSnapshot) (carrierquery.Rule, bool) {
	wanted, err := carrierquery.PLMNKey(snapshot.MCC, snapshot.MNC)
	if err != nil {
		return carrierquery.Rule{}, false
	}
	sort.SliceStable(rules, func(i, j int) bool {
		return strings.TrimSpace(rules[i].SPN) != "" && strings.TrimSpace(rules[j].SPN) == ""
	})
	for _, rule := range rules {
		key, keyErr := carrierquery.PLMNKey(rule.MCC, rule.MNC)
		if keyErr != nil || key != wanted || !rule.Enabled {
			continue
		}
		if rule.SPN != "" && !strings.EqualFold(strings.TrimSpace(rule.SPN), strings.TrimSpace(snapshot.SPN)) {
			continue
		}
		return rule, true
	}
	return carrierquery.Rule{}, false
}
