package eap

import "errors"

func NewFastReauthContext() *FastReauthContext {
	return &FastReauthContext{}
}

// SaveReauthData supports the original (string, mk, kEncr, kAut) form and the
// later ([]byte identity, []byte reauthID, []byte mk) reconstruction.
func (c *FastReauthContext) SaveReauthData(identity any, material ...[]byte) {
	switch value := identity.(type) {
	case string:
		c.saveOriginal(value, material)
	case []byte:
		c.saveCompatibility(value, material)
	default:
		panic("eap: invalid reauthentication identity")
	}
}

func (c *FastReauthContext) saveOriginal(reauthID string, material [][]byte) {
	if len(material) != 3 {
		panic("eap: SaveReauthData requires MK, KEncr, and KAut")
	}
	c.ReauthID = reauthID
	c.MK = material[0]
	c.KEncr = material[1]
	c.KAut = material[2]
	c.Enabled = true
	c.Counter = 0
}

func (c *FastReauthContext) saveCompatibility(_ []byte, material [][]byte) {
	if len(material) != 2 {
		panic("eap: compatibility SaveReauthData requires reauth ID and MK")
	}
	c.ReauthID = string(append([]byte(nil), material[0]...))
	c.MK = append([]byte(nil), material[1]...)
	c.Enabled = true
	c.Counter = 0
}

func (c *FastReauthContext) CanUseReauth() bool {
	return c.Enabled && c.ReauthID != ""
}

// BuildReauthResponse supports the original (nonceS, counter,
// counterTooSmall) form and the later (counter, counterTooSmall, mac) form.
func (c *FastReauthContext) BuildReauthResponse(first, second, third any) ([]byte, error) {
	switch value := first.(type) {
	case []byte:
		counter, okCounter := reauthCounter(second)
		counterTooSmall, okSmall := third.(bool)
		if !okCounter || !okSmall {
			return nil, errors.New("eap: invalid reauthentication arguments")
		}
		return c.buildOriginal(value, counter, counterTooSmall), nil
	case uint16:
		counterTooSmall, okSmall := second.(bool)
		mac, okMAC := third.([]byte)
		if !okSmall || !okMAC {
			return nil, errors.New("eap: invalid compatibility arguments")
		}
		return c.buildCompatibility(value, counterTooSmall, mac)
	case int:
		counter, okCounter := reauthCounter(value)
		counterTooSmall, okSmall := second.(bool)
		mac, okMAC := third.([]byte)
		if !okCounter || !okSmall || !okMAC {
			return nil, errors.New("eap: invalid compatibility arguments")
		}
		return c.buildCompatibility(counter, counterTooSmall, mac)
	default:
		return nil, errors.New("eap: invalid reauthentication arguments")
	}
}

func reauthCounter(value any) (uint16, bool) {
	switch counter := value.(type) {
	case uint16:
		return counter, true
	case int:
		if counter >= 0 && counter <= int(^uint16(0)) {
			return uint16(counter), true
		}
	}
	return 0, false
}

func (c *FastReauthContext) buildOriginal(nonceS []byte, counter uint16, counterTooSmall bool) []byte {
	c.NonceS = nonceS
	c.CounterSmall = counterTooSmall
	if !counterTooSmall {
		c.Counter = counter
	}
	return reauthAttributes(counter, counterTooSmall, nil)
}

func (c *FastReauthContext) buildCompatibility(counter uint16, counterTooSmall bool, mac []byte) ([]byte, error) {
	if len(mac) != 16 {
		return nil, errors.New("eap: AT_MAC must be 16 bytes")
	}
	c.Counter = counter
	return reauthAttributes(counter, counterTooSmall, mac), nil
}

func reauthAttributes(counter uint16, counterTooSmall bool, mac []byte) []byte {
	response := []byte{AT_COUNTER, 1, byte(counter >> 8), byte(counter)}
	if counterTooSmall {
		response = append(response, AT_COUNTER_TOO_SMALL, 1, 0, 0)
	}
	response = append(response, AT_MAC, 5, 0, 0)
	if mac == nil {
		return append(response, make([]byte, 16)...)
	}
	return append(response, mac...)
}
