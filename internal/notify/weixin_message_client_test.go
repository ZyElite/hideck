package notify

import (
	"encoding/json"
	"testing"
)

func TestWeixinUpdatesAcceptNumericMessageID(t *testing.T) {
	var response weixinUpdatesResponse
	payload := []byte(`{"ret":0,"msgs":[{"from_user_id":"user-1","to_user_id":"bot-1","message_id":123456,"message_type":1,"item_list":[{"type":1,"text_item":{"text":"hello"}}]}]}`)
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("unmarshal numeric message_id: %v", err)
	}
	if response.Messages[0].MessageID.String() != "123456" {
		t.Fatalf("message_id = %q", response.Messages[0].MessageID)
	}
}

func TestWeixinUpdatesAcceptStringMessageID(t *testing.T) {
	var response weixinUpdatesResponse
	payload := []byte(`{"ret":0,"msgs":[{"from_user_id":"user-1","to_user_id":"bot-1","message_id":"message-1","message_type":1}]}`)
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("unmarshal string message_id: %v", err)
	}
	if response.Messages[0].MessageID.String() != "message-1" {
		t.Fatalf("message_id = %q", response.Messages[0].MessageID)
	}
}
