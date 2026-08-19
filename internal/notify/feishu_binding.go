package notify

import (
	"reflect"
	"strings"

	"github.com/yibaiba/hideck/internal/config"
)

// TrustedFeishuBinding is the chat/user pair that load, persist, and QR apply share.
type TrustedFeishuBinding struct {
	ChatIDs      []string
	AllowedUsers []string
}

// TrustedFeishuBindingFrom keeps configured chats, plus runtime chats only after
// a verified owner binding. Leftover chats from an unverified first-speaker bind
// are ignored instead of being merged back in.
func TrustedFeishuBindingFrom(cfg config.FeishuConfig, state FeishuRuntimeState) TrustedFeishuBinding {
	configured := mergeUniqueStrings(cfg.ChatIDs, []string{cfg.ChatID})
	allowed := mergeUniqueStrings(state.AllowedUsers)
	chats := configured
	if state.BindingVerified && len(allowed) > 0 {
		chats = mergeUniqueStrings(configured, state.ChatIDs)
	}
	return TrustedFeishuBinding{ChatIDs: chats, AllowedUsers: allowed}
}

// ApplyFeishuDirectBinding appends a private-chat sender and chat onto the
// trusted binding and marks the binding verified.
func ApplyFeishuDirectBinding(cfg config.FeishuConfig, state FeishuRuntimeState, chatID, senderID string) FeishuRuntimeState {
	binding := TrustedFeishuBindingFrom(cfg, state)
	return FeishuRuntimeState{
		ChatIDs:         mergeUniqueStrings(binding.ChatIDs, []string{chatID}),
		AllowedUsers:    mergeUniqueStrings(binding.AllowedUsers, []string{senderID}),
		BindingVerified: true,
	}
}

// ApplyFeishuQRUserBinding records the scanned Open ID without trusting leftover
// unverified runtime chats.
func ApplyFeishuQRUserBinding(cfg config.FeishuConfig, state FeishuRuntimeState, openID string) FeishuRuntimeState {
	binding := TrustedFeishuBindingFrom(cfg, state)
	return FeishuRuntimeState{
		ChatIDs:         binding.ChatIDs,
		AllowedUsers:    mergeUniqueStrings(binding.AllowedUsers, []string{openID}),
		BindingVerified: true,
	}
}

func sameFeishuBinding(left, right FeishuRuntimeState) bool {
	return reflect.DeepEqual(left.ChatIDs, right.ChatIDs) &&
		reflect.DeepEqual(left.AllowedUsers, right.AllowedUsers) &&
		left.BindingVerified == right.BindingVerified
}

func mergeUniqueStrings(groups ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, group := range groups {
		for _, value := range group {
			id := strings.TrimSpace(value)
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}
