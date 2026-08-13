package phone

import (
	"fmt"

	"github.com/pion/sdp/v3"
)

const defaultMediaDirection = "sendrecv"

func browserOfferReceivesOnlyAudio(raw string) (bool, error) {
	description := &sdp.SessionDescription{}
	if err := description.Unmarshal([]byte(raw)); err != nil {
		return false, fmt.Errorf("phone: parse browser SDP direction: %w", err)
	}
	direction := mediaDirection(description.Attributes, defaultMediaDirection)
	for _, media := range description.MediaDescriptions {
		if media.MediaName.Media != "audio" {
			continue
		}
		return mediaDirection(media.Attributes, direction) == "recvonly", nil
	}
	return false, nil
}

func mediaDirection(attributes []sdp.Attribute, fallback string) string {
	for _, attribute := range attributes {
		switch attribute.Key {
		case "inactive", "recvonly", "sendonly", "sendrecv":
			return attribute.Key
		}
	}
	return fallback
}
