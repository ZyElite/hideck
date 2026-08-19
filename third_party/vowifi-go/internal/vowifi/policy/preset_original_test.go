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
		"csl_454000.yaml":        "250f9ec40e6c6a4203a5f29063491317c96276753c42980dd37c491f7d3cfc56",
		"cteuk_23433.yaml":       "52ec96bdca5e6e789862f47d9200eb5de3ddacd103c3374882898c8ec8b74517",
		"giffgaff_23410.yaml":    "011ead8b3e068fbabf34d05b0a6d11eb7e445ea9ff61f132a7fb0a80b22dd8c5",
		"lebara_uk_23487.yaml":   "517744d02af103bf396102297003d116998368100826879873bfb7e6456d8c91",
		"o2_de_26203.yaml":       "4772a3f0babe9f5da7cc160315f3344b63e0221b1d94fb8d18ee8739eec0d5a6",
		"o2_de_26207_alias.yaml": "a9a182ff9bc59e262d0c348dc8a65ff9c86cdd8ded3fe04803f1c1b7ea214cf2",
		"one_nz_53001.yaml":      "3a8bf0b20d12a121d226621472b855166208426bb75ff58b4b9fef67480340f5",
		"spark_nz_53005.yaml":    "6f155b103e975d8355ed1a35ec3b4b4ecf508187fd88cddc281f697fb300a443",
		"sunrise_22802.yaml":     "2160911e6d4fca664fbc9c474eee71a468d282703760f3c87e99df9043872e7b",
		"three_hk_454003.yaml":   "a3a84a436646cddd80df30aa2493f4b4076f08664a478d0b954c4142ef196d61",
		"three_uk_234020.yaml":   "a1f0ab61edf2ae016675fda613f6b6af45e4f40de035b598c3ec4c35796d527f",
		"tmobile_310240.yaml":    "bed3bfcb871282eab4c446004c1837c39426aa94e1206ab18c41f52aa82c4cb1",
		"tmobile_310260.yaml":    "57619113de3cfbe9ddb4afda13885501415f534f580fc55a8e7f54a15f2b93a6",
		"vodafone_nl_20404.yaml": "ca847e87abe516603cd92f05ef0a9e5ba8ad0869f7cca3ea2aabe9f49694df8b",
		"vodafone_uk_23415.yaml": "63fb1642b77a291de5795827200a1589117fed25fab843748a3bea39dc1d6a62",
	}
	entries, err := carrierPresetFiles.ReadDir("presets")
	if err != nil {
		t.Fatalf("read embedded preset directory: %v", err)
	}
	if len(entries) != len(wantHashes) {
		t.Fatalf("embedded preset files = %d, want %d", len(entries), len(wantHashes))
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
		"204004", "228002", "234010", "234015", "234020", "234033", "234087", "262003", "262007", "310240",
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
