package swu

import (
	"bytes"
	"errors"
	"fmt"
	"net"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

type childSAOffer struct {
	encryption        uint16
	encryptionKeyBits uint16
	integrity         uint16
	dhGroup           uint16
	tsi               *ikev2.EncryptedPayloadTS
	tsr               *ikev2.EncryptedPayloadTS
	localIPs          []net.IP
	requireSA         bool
	requireNonce      bool
}

type childSASelection struct {
	remoteSPI  uint32
	nonce      []byte
	tsi        *ikev2.EncryptedPayloadTS
	tsr        *ikev2.EncryptedPayloadTS
	encryption uint16
	integrity  uint16
	dhGroup    uint16
}

func validateChildSAResponse(payloads []ikev2.Payload, offer childSAOffer) (*childSASelection, error) {
	sa, nonce, tsi, tsr, err := collectChildSAPayloads(payloads)
	if err != nil {
		return nil, err
	}
	if sa == nil {
		if offer.requireSA {
			return nil, errors.New("swu: CHILD_SA response missing SA payload")
		}
		if tsi != nil || tsr != nil {
			return nil, errors.New("swu: CHILD_SA response has traffic selectors without SA")
		}
		return nil, nil
	}
	if len(sa.Proposals) != 1 {
		return nil, fmt.Errorf("swu: CHILD_SA response selected %d proposals, want 1", len(sa.Proposals))
	}
	spi, encryption, integrity, dhGroup, err := validateESPSelection(sa.Proposals[0], offer)
	if err != nil {
		return nil, err
	}
	if err := validateTrafficSelectorNarrowing("TSi", tsi, offer.tsi, ikev2.PayloadTSi); err != nil {
		return nil, err
	}
	if err := validateTrafficSelectorNarrowing("TSr", tsr, offer.tsr, ikev2.PayloadTSr); err != nil {
		return nil, err
	}
	if len(offer.localIPs) > 0 && !selectorsContainAnyIP(tsi, offer.localIPs) {
		return nil, errors.New("swu: selected TSi does not contain an assigned inner address")
	}
	if offer.requireNonce && len(nonce) == 0 {
		return nil, errors.New("swu: CREATE_CHILD_SA response missing nonce")
	}
	return &childSASelection{
		remoteSPI: spi, nonce: nonce,
		tsi: cloneTrafficSelectorPayload(tsi), tsr: cloneTrafficSelectorPayload(tsr),
		encryption: encryption, integrity: integrity, dhGroup: dhGroup,
	}, nil
}

func collectChildSAPayloads(payloads []ikev2.Payload) (*ikev2.EncryptedPayloadSA, []byte, *ikev2.EncryptedPayloadTS, *ikev2.EncryptedPayloadTS, error) {
	var sa *ikev2.EncryptedPayloadSA
	var nonce []byte
	seenNonce := false
	var tsi, tsr *ikev2.EncryptedPayloadTS
	for _, payload := range payloads {
		if payload == nil {
			return nil, nil, nil, nil, errors.New("swu: CHILD_SA response contains a nil payload")
		}
		switch payload.Type() {
		case ikev2.PayloadSA:
			value, ok := payload.(*ikev2.EncryptedPayloadSA)
			if !ok || sa != nil {
				return nil, nil, nil, nil, errors.New("swu: invalid or duplicate CHILD_SA SA payload")
			}
			sa = value
		case ikev2.PayloadNi:
			if seenNonce {
				return nil, nil, nil, nil, errors.New("swu: duplicate CHILD_SA nonce payload")
			}
			seenNonce = true
			nonce = childSANonceData(payload)
		case ikev2.PayloadTSi, ikev2.PayloadTSr:
			value, ok := payload.(*ikev2.EncryptedPayloadTS)
			if !ok {
				return nil, nil, nil, nil, errors.New("swu: invalid CHILD_SA traffic selector payload")
			}
			if payload.Type() == ikev2.PayloadTSi {
				if tsi != nil {
					return nil, nil, nil, nil, errors.New("swu: duplicate CHILD_SA TSi payload")
				}
				tsi = value
			} else {
				if tsr != nil {
					return nil, nil, nil, nil, errors.New("swu: duplicate CHILD_SA TSr payload")
				}
				tsr = value
			}
		}
	}
	return sa, nonce, tsi, tsr, nil
}

func childSANonceData(payload ikev2.Payload) []byte {
	switch value := payload.(type) {
	case *ikev2.EncryptedPayloadNonce:
		return append([]byte(nil), value.NonceData...)
	case *ikev2.RawPayload:
		return append([]byte(nil), value.Data...)
	default:
		return nil
	}
}

func validateTrafficSelectorNarrowing(
	name string,
	selected, offered *ikev2.EncryptedPayloadTS,
	payloadType ikev2.PayloadType,
) error {
	if selected == nil || offered == nil || selected.Type() != payloadType || len(selected.TrafficSelectors) == 0 {
		return fmt.Errorf("swu: CHILD_SA response missing valid %s", name)
	}
	for _, selector := range selected.TrafficSelectors {
		contained := false
		for _, offeredSelector := range offered.TrafficSelectors {
			if trafficSelectorContained(selector, offeredSelector) {
				contained = true
				break
			}
		}
		if !contained {
			return fmt.Errorf("swu: CHILD_SA %s is not a legal narrowing", name)
		}
	}
	return nil
}

func trafficSelectorContained(selected, offered *ikev2.TrafficSelector) bool {
	if selected == nil || offered == nil || selected.TSType != offered.TSType || len(selected.StartAddr) != len(offered.StartAddr) ||
		len(selected.EndAddr) != len(offered.EndAddr) || bytes.Compare(selected.StartAddr, selected.EndAddr) > 0 ||
		selected.StartPort > selected.EndPort {
		return false
	}
	if offered.IPProtocol != 0 && selected.IPProtocol != offered.IPProtocol {
		return false
	}
	return selected.StartPort >= offered.StartPort && selected.EndPort <= offered.EndPort &&
		bytes.Compare(selected.StartAddr, offered.StartAddr) >= 0 && bytes.Compare(selected.EndAddr, offered.EndAddr) <= 0
}

func selectorsContainAnyIP(payload *ikev2.EncryptedPayloadTS, ips []net.IP) bool {
	if payload == nil || len(ips) == 0 {
		return false
	}
	for _, selector := range payload.TrafficSelectors {
		for _, ip := range ips {
			address := ip.To16()
			if selector.TSType == ikev2.TS_IPV4_ADDR_RANGE {
				address = ip.To4()
			}
			if len(address) == len(selector.StartAddr) && bytes.Compare(address, selector.StartAddr) >= 0 && bytes.Compare(address, selector.EndAddr) <= 0 {
				return true
			}
		}
	}
	return false
}

func cloneTrafficSelectorPayload(payload *ikev2.EncryptedPayloadTS) *ikev2.EncryptedPayloadTS {
	if payload == nil {
		return nil
	}
	clone := &ikev2.EncryptedPayloadTS{IsInitiator: payload.IsInitiator}
	for _, selector := range payload.TrafficSelectors {
		copied := *selector
		copied.StartAddr = append([]byte(nil), selector.StartAddr...)
		copied.EndAddr = append([]byte(nil), selector.EndAddr...)
		clone.TrafficSelectors = append(clone.TrafficSelectors, &copied)
	}
	return clone
}
