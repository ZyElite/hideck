package notify

import (
	"reflect"
	"testing"

	"github.com/yibaiba/hideck/internal/config"
)

func TestTrustedFeishuBindingFromIgnoresUnverifiedRuntimeChats(t *testing.T) {
	cfg := config.FeishuConfig{ChatIDs: []string{"oc_configured"}, ChatID: "oc_legacy"}
	got := TrustedFeishuBindingFrom(cfg, FeishuRuntimeState{
		ChatIDs: []string{"oc_unverified"}, AllowedUsers: []string{"ou_owner"},
	})
	if !reflect.DeepEqual(got.ChatIDs, []string{"oc_configured", "oc_legacy"}) {
		t.Fatalf("unverified chats = %#v", got.ChatIDs)
	}
	if !reflect.DeepEqual(got.AllowedUsers, []string{"ou_owner"}) {
		t.Fatalf("allowed users = %#v", got.AllowedUsers)
	}
}

func TestTrustedFeishuBindingFromMergesVerifiedRuntimeChats(t *testing.T) {
	cfg := config.FeishuConfig{ChatIDs: []string{"oc_configured"}}
	got := TrustedFeishuBindingFrom(cfg, FeishuRuntimeState{
		ChatIDs: []string{"oc_verified"}, AllowedUsers: []string{"ou_owner"},
		BindingVerified: true,
	})
	if !reflect.DeepEqual(got.ChatIDs, []string{"oc_configured", "oc_verified"}) {
		t.Fatalf("verified chats = %#v", got.ChatIDs)
	}
}

func TestApplyFeishuDirectBindingPersistsOnlyRuntimeChats(t *testing.T) {
	cfg := config.FeishuConfig{ChatIDs: []string{"oc_configured"}}
	got := ApplyFeishuDirectBinding(cfg, FeishuRuntimeState{
		ChatIDs:         []string{"oc_configured", "oc_runtime"},
		AllowedUsers:    []string{"ou_existing"},
		BindingVerified: true,
	}, "oc_owner", "ou_owner")
	if !reflect.DeepEqual(got.ChatIDs, []string{"oc_runtime", "oc_owner"}) {
		t.Fatalf("bound chats = %#v", got.ChatIDs)
	}
	if !reflect.DeepEqual(got.AllowedUsers, []string{"ou_existing", "ou_owner"}) || !got.BindingVerified {
		t.Fatalf("bound state = %+v", got)
	}
}

func TestApplyFeishuQRUserBindingDropsUnverifiedChats(t *testing.T) {
	cfg := config.FeishuConfig{ChatIDs: []string{"oc_configured"}}
	got := ApplyFeishuQRUserBinding(cfg, FeishuRuntimeState{
		ChatIDs: []string{"oc_unverified"},
	}, "ou_owner")
	if len(got.ChatIDs) != 0 {
		t.Fatalf("qr chats = %#v", got.ChatIDs)
	}
	if !reflect.DeepEqual(got.AllowedUsers, []string{"ou_owner"}) || !got.BindingVerified {
		t.Fatalf("qr state = %+v", got)
	}
	if reloaded := TrustedFeishuBindingFrom(config.FeishuConfig{}, got); len(reloaded.ChatIDs) != 0 {
		t.Fatalf("removed configured chat was restored: %#v", reloaded.ChatIDs)
	}
}
