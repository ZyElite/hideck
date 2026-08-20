package imscore

import (
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

func (s *Service) logRegisterFlowNegotiation(response *sipResponse) {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return
	}
	supported := strings.ToLower(strings.Join(response.HeaderValues("Supported"), ","))
	require := strings.ToLower(strings.Join(response.HeaderValues("Require"), ","))
	path := strings.ToLower(strings.Join(response.HeaderValues("Path"), ","))
	contact := strings.ToLower(strings.Join(response.HeaderValues("Contact"), ","))
	logging.Info("IMS REGISTER flow negotiation",
		"device", s.DeviceID(),
		"supported_outbound", containsHeaderToken(supported, "outbound"),
		"required_outbound", containsHeaderToken(require, "outbound"),
		"path_present", strings.TrimSpace(path) != "",
		"path_ob", containsSIPParameter(path, "ob"),
		"contact_reg_id", containsSIPParameter(contact, "reg-id"))
}

func containsHeaderToken(value, target string) bool {
	for token := range strings.SplitSeq(value, ",") {
		if strings.EqualFold(strings.TrimSpace(token), target) {
			return true
		}
	}
	return false
}

func containsSIPParameter(value, parameter string) bool {
	parameter = strings.ToLower(strings.TrimSpace(parameter))
	if parameter == "" {
		return false
	}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == ',' || r == '>' || r == '<'
	}) {
		name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
		if strings.EqualFold(name, parameter) {
			return true
		}
	}
	return false
}
