package startup

import (
	"testing"

	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

type carrierStartupExpectation struct {
	name, mcc, mnc, normalizedMNC, imsi, presetID string
	epdg, epdgSource, espFirst, templateID        string
	e911Provider, akaMode                         string
	dpdSeconds, reauthSeconds                     int
}

var originalCarrierStartupExpectations = []carrierStartupExpectation{
	{name: "Vodafone NL", mcc: "204", mnc: "04", normalizedMNC: "004", imsi: "204040000000001", presetID: "vodafone_nl_20404", epdg: "epdg.epc.mnc004.mcc204.pub.3gppnetwork.org", epdgSource: "preset", espFirst: "aes256-sha256", templateID: "vodafone_nl_20404_ios", akaMode: "minimal", dpdSeconds: 600},
	{name: "Sunrise", mcc: "228", mnc: "002", normalizedMNC: "002", imsi: "228020000000001", presetID: "sunrise_22802", epdg: "epdg.epc.mnc002.mcc228.pub.3gppnetwork.org", epdgSource: "standard", espFirst: "aes128-sha256", templateID: "defaultIMSRegisterTemplate", akaMode: "checkcode", dpdSeconds: 120},
	{name: "giffgaff", mcc: "234", mnc: "10", normalizedMNC: "010", imsi: "234102356143376", presetID: "giffgaff_23410", epdg: "epdg.epc.mnc010.mcc234.pub.3gppnetwork.org", epdgSource: "standard", espFirst: "aes256-sha512", templateID: "giffgaff", akaMode: "minimal", dpdSeconds: 120},
	{name: "Three UK", mcc: "234", mnc: "020", normalizedMNC: "020", imsi: "234200000000001", presetID: "three_uk_234020", epdg: "epdg.epc.mnc020.mcc234.pub.3gppnetwork.org", epdgSource: "standard", espFirst: "aes128-sha256", templateID: "three_uk_234020", akaMode: "minimal", dpdSeconds: 600},
	{name: "CTExcel", mcc: "234", mnc: "33", normalizedMNC: "033", imsi: "234336575868434", presetID: "CTEUK_23433", epdg: "epdg.epc.mnc033.mcc234.pub.3gppnetwork.org", epdgSource: "standard", espFirst: "aes256gcm16", templateID: "CTEUK_23433", akaMode: "minimal", dpdSeconds: 120},
	{name: "O2 DE 03", mcc: "262", mnc: "03", normalizedMNC: "003", imsi: "262030000000001", presetID: "O2_de_26203", epdg: "epdg.epc.mnc003.mcc262.pub.3gppnetwork.org", epdgSource: "preset", espFirst: "aes256-sha256", templateID: "O2_de_26203_ios", akaMode: "minimal", dpdSeconds: 600},
	{name: "O2 DE 07", mcc: "262", mnc: "07", normalizedMNC: "007", imsi: "262070000000001", presetID: "O2_de_26207_alias", epdg: "epdg.epc.mnc007.mcc262.pub.3gppnetwork.org", epdgSource: "preset", espFirst: "aes256-sha256", templateID: "O2_de_26203", akaMode: "minimal", dpdSeconds: 600},
	{name: "T-Mobile 240", mcc: "310", mnc: "240", normalizedMNC: "240", imsi: "310240000000001", presetID: "T-Mobile_240", epdg: "epdg.epc.mnc240.mcc310.pub.3gppnetwork.org", epdgSource: "standard", espFirst: "aes128-sha256", templateID: "defaultIMSRegisterTemplate", e911Provider: "T-Mobile_entitlement", akaMode: "minimal", dpdSeconds: 120},
	{name: "T-Mobile 260", mcc: "310", mnc: "260", normalizedMNC: "260", imsi: "310260000000001", presetID: "T-Mobile_260", epdg: "epdg.epc.mnc260.mcc310.pub.3gppnetwork.org", epdgSource: "standard", espFirst: "aes128-sha256", templateID: "defaultIMSRegisterTemplate", e911Provider: "T-Mobile_entitlement", akaMode: "minimal", dpdSeconds: 120},
	{name: "AT&T 280", mcc: "310", mnc: "280", normalizedMNC: "280", imsi: "310280233621715", presetID: "att_310280", epdg: "epdg.epc.att.net", epdgSource: "preset", espFirst: "aes128-sha256", templateID: "att_310280", e911Provider: "att_entitlement", akaMode: "minimal", dpdSeconds: 600},
	{name: "LycaMobile AT&T", mcc: "310", mnc: "410", normalizedMNC: "410", imsi: "310410000000001", presetID: "LycaMobile_310410", epdg: "epdg.epc.att.net", epdgSource: "preset", espFirst: "aes128-sha256", templateID: "att_310280", e911Provider: "att_entitlement", akaMode: "minimal", dpdSeconds: 600},
	{name: "csl", mcc: "454", mnc: "000", normalizedMNC: "000", imsi: "454000000000001", presetID: "csl_454000", epdg: "epdg.epc.mnc000.mcc454.pub.3gppnetwork.org", epdgSource: "standard", espFirst: "aes256-sha256", templateID: "csl_454000", akaMode: "omit", dpdSeconds: 120},
	{name: "Three HK", mcc: "454", mnc: "003", normalizedMNC: "003", imsi: "454030000000001", presetID: "three_hk_454003", epdg: "wlan.three.com.hk", epdgSource: "preset", espFirst: "aes256-sha256", templateID: "three_hk_454003", akaMode: "checkcode", dpdSeconds: 120},
	{name: "One NZ", mcc: "530", mnc: "01", normalizedMNC: "001", imsi: "530010000000001", presetID: "one_nz_53001", epdg: "epdg.epc.mnc001.mcc530.pub.3gppnetwork.org", epdgSource: "standard", espFirst: "aes256gcm16", templateID: "defaultIMSRegisterTemplate", akaMode: "minimal", dpdSeconds: 120},
	{name: "Spark NZ", mcc: "530", mnc: "05", normalizedMNC: "005", imsi: "530050000000001", presetID: "spark_nz_53005", epdg: "epdg.epc.mnc005.mcc530.pub.3gppnetwork.spark.co.nz", epdgSource: "preset", espFirst: "aes256-sha256", templateID: "spark_nz_53005_ios", akaMode: "minimal", dpdSeconds: 600},
	{name: "2degrees NZ", mcc: "530", mnc: "24", normalizedMNC: "024", imsi: "530240000000001", presetID: "2degrees_nz_53024", epdg: "epdg.ims.2degrees.net.nz", epdgSource: "preset", espFirst: "aes256-sha512", templateID: "2degrees_nz_53024_ios", akaMode: "minimal", dpdSeconds: 300},
}

func TestPrepareStartProjectsEveryOriginalCarrierPreset(t *testing.T) {
	identity := profile.IMSIdentityResult{
		RequestedSource: "isim", ActualSource: "isim", AKAAppPreference: profile.AKAAppISIMStrict,
		Applied: true, IMPI: "user@ims.example", IMPU: "sip:user@ims.example", Domain: "ims.example",
	}
	for _, expectation := range originalCarrierStartupExpectations {
		expectation := expectation
		t.Run(expectation.name, func(t *testing.T) {
			prepared, err := PrepareStart("device", profile.Profile{
				IMSI: expectation.imsi, MCC: expectation.mcc, MNC: expectation.mnc,
			}, "", identity, nil, nil)
			if err != nil {
				t.Fatalf("PrepareStart: %v", err)
			}
			assertOriginalCarrierProjection(t, prepared, expectation)
		})
	}
}

func assertOriginalCarrierProjection(
	t *testing.T,
	prepared profile.PreparedSession,
	want carrierStartupExpectation,
) {
	t.Helper()
	plan := prepared.CarrierPlan
	if plan.Metadata.MCC != want.mcc || plan.Metadata.MNC != want.normalizedMNC ||
		plan.Metadata.PresetID != want.presetID || plan.Metadata.MatchedTemplate != want.presetID {
		t.Fatalf("carrier metadata = %+v", plan.Metadata)
	}
	if prepared.EPDGAddr != want.epdg || prepared.EPDGSource != want.epdgSource {
		t.Fatalf("ePDG = %q source %q", prepared.EPDGAddr, prepared.EPDGSource)
	}
	if plan.IKE.DPDIntervalSeconds != want.dpdSeconds || plan.IKE.ReauthIntervalSeconds != want.reauthSeconds ||
		plan.IKE.AKAChallengeMode != want.akaMode || len(plan.IKE.ESPProposals) == 0 ||
		plan.IKE.ESPProposals[0] != want.espFirst {
		t.Fatalf("IKE projection = %+v", plan.IKE)
	}
	if plan.IMS.RegisterTemplate.ID != want.templateID || plan.E911.Provider != want.e911Provider {
		t.Fatalf("IMS/E911 projection: template=%q E911=%+v", plan.IMS.RegisterTemplate.ID, plan.E911)
	}
}
