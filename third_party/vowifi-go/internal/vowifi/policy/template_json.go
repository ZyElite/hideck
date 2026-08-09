package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type imsRegisterTemplateAlias IMSRegisterTemplate

// UnmarshalJSON accepts both the recovered structured fields and the interim
// scalar override fields that existed before the v1.5.5 type was restored.
func (t *IMSRegisterTemplate) UnmarshalJSON(data []byte) error {
	var decoded imsRegisterTemplateAlias
	fields := struct {
		*imsRegisterTemplateAlias
		RegisterPolicy json.RawMessage
		SecAgreeMode   json.RawMessage
	}{imsRegisterTemplateAlias: &decoded}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if err := decodeRegisterPolicy(fields.RegisterPolicy, &decoded); err != nil {
		return err
	}
	if err := decodeSecAgreeMode(fields.SecAgreeMode, &decoded); err != nil {
		return err
	}
	*t = IMSRegisterTemplate(decoded)
	return nil
}

func decodeRegisterPolicy(raw json.RawMessage, template *imsRegisterTemplateAlias) error {
	data := bytes.TrimSpace(raw)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	switch data[0] {
	case '"':
		return json.Unmarshal(data, &template.RegisterPolicyMode)
	case '{':
		return json.Unmarshal(data, &template.RegisterPolicy)
	default:
		return fmt.Errorf("policy: RegisterPolicy must be a string or object")
	}
}

func decodeSecAgreeMode(raw json.RawMessage, template *imsRegisterTemplateAlias) error {
	data := bytes.TrimSpace(raw)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	switch data[0] {
	case '"':
		return json.Unmarshal(data, &template.SecAgreeMode)
	case 't', 'f':
		return json.Unmarshal(data, &template.SecAgreeEnabled)
	default:
		return fmt.Errorf("policy: SecAgreeMode must be a string or boolean")
	}
}
