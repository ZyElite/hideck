package carrier

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestLoadCarrierOverridesRestoresYAMLContract(t *testing.T) {
	ClearCarrierOverrides()
	t.Cleanup(ClearCarrierOverrides)
	path := writeOverrideFile(t, "carrier.yaml", `carrier_overrides:
  "310-260":
    id: ordered_external
    ike_proposals: [aes256-sha512-prfsha512-modp2048, aes128-sha256-modp2048]
    esp_proposals: [aes256-sha512, aes128-sha256]
`)
	resolved, count, missing, err := LoadCarrierOverrides(path)
	if err != nil || resolved != path || count != 1 || missing {
		t.Fatalf("LoadCarrierOverrides() = %q %d %t %v", resolved, count, missing, err)
	}
	config := ResolveEffectiveCarrierConfig("310", "260")
	wantIKE := []string{"aes256-sha512-prfsha512-modp2048", "aes128-sha256-modp2048"}
	wantESP := []string{"aes256-sha512", "aes128-sha256"}
	if config.PresetID != "ordered_external" || !reflect.DeepEqual(config.IKEProposals, wantIKE) ||
		!reflect.DeepEqual(config.ESPProposals, wantESP) {
		t.Fatalf("resolved YAML override = %+v", config)
	}
}

func TestYAMLLoadFailureDoesNotReplaceActiveOverrides(t *testing.T) {
	ClearCarrierOverrides()
	t.Cleanup(ClearCarrierOverrides)
	valid := writeOverrideFile(t, "valid.yaml", `"23410": {id: retained}`)
	if _, _, _, err := LoadCarrierOverrides(valid); err != nil {
		t.Fatalf("LoadCarrierOverrides(valid) error = %v", err)
	}
	invalid := writeOverrideFile(t, "invalid.yaml", `bad: {id: rejected}`)
	if _, _, _, err := LoadCarrierOverrides(invalid); err == nil {
		t.Fatal("LoadCarrierOverrides(invalid) succeeded")
	}
	if got := ResolveEffectiveCarrierConfig("234", "10").PresetID; got != "retained" {
		t.Fatalf("failed load replaced active override: %q", got)
	}
}

func TestLoadCarrierOverridesJSONCompatibility(t *testing.T) {
	ClearCarrierOverrides()
	t.Cleanup(ClearCarrierOverrides)
	path := writeOverrideFile(t, "carrier.json", `[
  {"MCC":"310","MNC":"260","PresetID":"json_external",
   "IKEProposals":["aes256-sha512-prfsha512-modp2048","aes128-sha256-modp2048"],
   "ESPProposals":["aes256-sha512","aes128-sha256"],
   "IMS":{"ExpiresSeconds":321,"Transport":"tcp"}}
]`)
	result, err := LoadCarrierOverridesCurrent(path)
	if err != nil || result.Path != path || result.Count != 1 || result.Missing {
		t.Fatalf("LoadCarrierOverridesResult() = %+v %v", result, err)
	}
	config := ResolveEffectiveCarrierConfig("310", "260")
	if config.PresetID != "json_external" || config.IKEProposals[0] != "aes256-sha512-prfsha512-modp2048" ||
		config.IKEProposals[1] != "aes128-sha256-modp2048" || config.IMSTransport != "tcp" ||
		config.IMS.ExpiresSeconds != 321 {
		t.Fatalf("resolved JSON override = %+v", config)
	}
}

func TestJSONOverrideValidationIsAtomic(t *testing.T) {
	ClearCarrierOverrides()
	t.Cleanup(ClearCarrierOverrides)
	valid := writeOverrideFile(t, "valid.json", `[{"MCC":"234","MNC":"10","PresetID":"retained"}]`)
	if _, err := LoadCarrierOverridesJSON(valid); err != nil {
		t.Fatalf("LoadCarrierOverridesJSON(valid) error = %v", err)
	}
	tests := []struct{ name, body, want string }{
		{"zero expiry", `[{"MCC":"234","MNC":"10","IMS":{"ExpiresSeconds":0}}]`, "expiry must be positive"},
		{"invalid proposal", `[{"MCC":"234","MNC":"10","IKEProposals":["unknown"]}]`, "IKE proposals"},
		{"duplicate PLMN", `[{"MCC":"234","MNC":"10"},{"MCC":"234","MNC":"010"}]`, "duplicate PLMN"},
		{"unknown field", `[{"MCC":"234","MNC":"10","Unknown":true}]`, "unknown field"},
		{"unknown IMS field", `[{"MCC":"234","MNC":"10","IMS":{"Unknown":true}}]`, "unknown field"},
		{"invalid SMS receiver", `[{"MCC":"234","MNC":"10","IMS":{"SMSReceiverTransport":"sctp"}}]`, "unsupported SMS receiver"},
		{"invalid sec agree", `[{"MCC":"234","MNC":"10","IMS":{"SecAgreeMode":"sometimes"}}]`, "unsupported sec-agree"},
		{"invalid status", `[{"MCC":"234","MNC":"10","IMS":{"RegisterPolicy":{"TemporaryStatusCodes":[700]}}}]`, "invalid REGISTER temporary status"},
		{"duplicate mechanism", `[{"MCC":"234","MNC":"10","IMS":{"SecurityClientMechanisms":[{"Alg":"hmac-md5-96","EAlg":"aes-cbc","Prot":"esp","Mode":"trans"},{"Alg":"hmac(md5)","EAlg":"cbc(aes)","Prot":"ESP","Mode":"TRANS"}]}}]`, "duplicate Security-Client"},
		{"invalid E911 URL", `[{"MCC":"310","MNC":"280","E911":{"Websheet":"not-a-url"}}]`, "E911 websheet"},
		{"incomplete E911", `[{"MCC":"001","MNC":"01","E911":{"Enabled":true}}]`, "enabled E911 has no provider"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name := strings.ReplaceAll(test.name, " ", "_") + ".json"
			path := writeOverrideFile(t, name, test.body)
			if _, err := LoadCarrierOverridesJSON(path); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadCarrierOverridesJSON() error = %v, want %q", err, test.want)
			}
			if got := ResolveEffectiveCarrierConfig("234", "10").PresetID; got != "retained" {
				t.Fatalf("failed JSON load replaced active override: %q", got)
			}
		})
	}
}

func TestCarrierOverrideStoreConcurrentResolution(t *testing.T) {
	ClearCarrierOverrides()
	t.Cleanup(ClearCarrierOverrides)
	paths := []string{
		writeOverrideFile(t, "a.json", `[{"MCC":"234","MNC":"10","PresetID":"a"}]`),
		writeOverrideFile(t, "b.json", `[{"MCC":"234","MNC":"10","PresetID":"b"}]`),
	}
	const iterations = 100
	errors := make(chan error, 3)
	var workers sync.WaitGroup
	for _, path := range paths {
		workers.Add(1)
		go loadOverridesRepeatedly(path, iterations, &workers, errors)
	}
	workers.Add(1)
	go resolveRepeatedly(iterations*2, &workers, errors)
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}

func loadOverridesRepeatedly(path string, iterations int, workers *sync.WaitGroup, errors chan<- error) {
	defer workers.Done()
	for index := 0; index < iterations; index++ {
		if _, err := LoadCarrierOverridesJSON(path); err != nil {
			errors <- err
			return
		}
	}
}

func resolveRepeatedly(iterations int, workers *sync.WaitGroup, errors chan<- error) {
	defer workers.Done()
	for index := 0; index < iterations; index++ {
		config := ResolveEffectiveCarrierConfig("234", "10")
		if len(config.IKEProposals) == 0 {
			errors <- fmt.Errorf("resolved empty IKE proposal list")
			return
		}
		config.IKEProposals[0] = "caller mutation"
	}
}

func writeOverrideFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
