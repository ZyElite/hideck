package policy

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestOriginalCarrierPresetAssetsRemainExact(t *testing.T) {
	wantHashes := map[string]string{
		"2degrees_nz_53024.yaml": "40ba4fc4b21aed123cb5d2c3634a3a4a0d7d7a33046dd2e356e8950e6250c471",
		"att_310280.yaml":        "7337399037af7fbe67874b83f8f0c95ff0a1c588ec49ee41a404cf33e04fd054",
		"att_310410.yaml":        "06c030e8bd636271f9a23400cd4a622187cb38792fdc01b5422a4d719ff59b95",
		"cteuk_23433.yaml":       "52ec96bdca5e6e789862f47d9200eb5de3ddacd103c3374882898c8ec8b74517",
		"sunrise_22802.yaml":     "2160911e6d4fca664fbc9c474eee71a468d282703760f3c87e99df9043872e7b",
	}
	for name, want := range wantHashes {
		data, err := carrierPresetFiles.ReadFile("presets/" + name)
		if err != nil {
			t.Fatalf("read original preset %s: %v", name, err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			t.Errorf("original preset %s hash = %s, want %s", name, got, want)
		}
	}
}

func TestEmbeddedCarrierPresetInventory(t *testing.T) {
	want := []string{
		"204004", "228002", "234010", "234020", "234033", "262003", "262007", "310240",
		"310260", "310280", "310410", "454000", "454003", "530001", "530005", "530024",
	}
	got := make([]string, 0, len(embeddedCarrierPresets))
	for key := range embeddedCarrierPresets {
		got = append(got, key)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("embedded carrier preset inventory = %v, want %v", got, want)
	}
}
