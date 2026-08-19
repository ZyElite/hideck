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
		chats = mergeUniqueStrings(configured, runtimeFeishuChatIDs(cfg, state))
	}
	return TrustedFeishuBinding{ChatIDs: chats, AllowedUsers: allowed}
}

// ApplyFeishuDirectBinding records an automatically discovered private chat
// without duplicating statically configured notification targets.
func ApplyFeishuDirectBinding(cfg config.FeishuConfig, state FeishuRuntimeState, chatID, senderID string) FeishuRuntimeState {
	return FeishuRuntimeState{
		ChatIDs:         mergeUniqueStrings(runtimeFeishuChatIDs(cfg, state), []string{chatID}),
		AllowedUsers:    mergeUniqueStrings(state.AllowedUsers, []string{senderID}),
		BindingVerified: true,
	}
}

// ApplyFeishuQRUserBinding records the scanned Open ID without copying static or
// leftover unverified chats into runtime state.
func ApplyFeishuQRUserBinding(cfg config.FeishuConfig, state FeishuRuntimeState, openID string) FeishuRuntimeState {
	return FeishuRuntimeState{
		ChatIDs:         runtimeFeishuChatIDs(cfg, state),
		AllowedUsers:    mergeUniqueStrings(state.AllowedUsers, []string{openID}),
		BindingVerified: true,
	}
}

func runtimeFeishuChatIDs(cfg config.FeishuConfig, state FeishuRuntimeState) []string {
	if !state.BindingVerified || len(mergeUniqueStrings(state.AllowedUsers)) == 0 {
		return nil
	}
	configured := mergeUniqueStrings(cfg.ChatIDs, []string{cfg.ChatID})
	configuredSet := make(map[string]struct{}, len(configured))
	for _, chatID := range configured {
		configuredSet[chatID] = struct{}{}
	}
	runtime := make([]string, 0, len(state.ChatIDs))
	for _, chatID := range mergeUniqueStrings(state.ChatIDs) {
		if _, isConfigured := configuredSet[chatID]; !isConfigured {
			runtime = append(runtime, chatID)
		}
	}
	return runtime
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
