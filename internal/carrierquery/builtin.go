package carrierquery

const (
	verifiedFree = "verified_free"
	costUnknown  = "unknown"
	official     = "official"
	projectTest  = "project_real_test"
)

var builtInRules = []Rule{
	smsRule("2degrees_nz_53024", "530", "24", "2degrees NZ", "233", "BAL", "NZD", verifiedFree,
		"https://www.2degrees.nz/help/account-and-billing/manage-account/ussd-shutdown", []string{"233", "2degrees"}),
	ussdSMSRule("att_310280", "310", "280", "AT&T", "*777#", "USD",
		"https://www.att.com/support/article-modal/wireless/KM1048283/", []string{"AT&T"}, "USSD 请求后由运营商短信返回结果"),
	ussdSMSRule("att_310410", "310", "410", "AT&T", "*777#", "USD",
		"https://www.att.com/support/article-modal/wireless/KM1048283/", []string{"AT&T"}, "USSD 请求后由运营商短信返回结果"),
	unsupportedRule("csl_454000", "454", "000", "CSL Hong Kong", "不同预付卡产品使用 *109# 或 ##122#，需先确认卡产品",
		"https://www.hkcsl.com/en/Recharge-method/", "查询码按产品变化，不能自动选择"),
	smsProjectRule("cteuk_23433", "234", "33", "CTExcel UK", "888", "BAL", "GBP", []string{"888", "CTExcel"},
		"VoHive 实机验证：BAL 发往 888；官网仅确认 888 为免费服务热线"),
	smsRule("giffgaff_23410", "234", "10", "giffgaff", "85075", "INFO", "GBP", costUnknown,
		"https://help.giffgaff.com/en/articles/258872-guide-to-the-usage-statement", []string{"85075", "giffgaff"}),
	unsupportedRule("o2_de_26203", "262", "03", "O2 Germany", "S/M/L 套餐使用 *105#，其他套餐使用 *101#",
		"https://www.o2online.de/service/guthaben-aufladen/", "查询码取决于套餐，当前无法从 SIM 身份可靠判定"),
	unsupportedRule("o2_de_26207", "262", "07", "O2 Germany", "S/M/L 套餐使用 *105#，其他套餐使用 *101#",
		"https://www.o2online.de/service/guthaben-aufladen/", "查询码取决于套餐，当前无法从 SIM 身份可靠判定"),
	smsRule("one_nz_53001", "530", "01", "One NZ", "777", "BAL", "NZD", verifiedFree,
		"https://one.nz/faq/manage-your-mobile-by-txt", []string{"777", "One NZ", "Vodafone"}),
	unsupportedRule("spark_nz_53005", "530", "05", "Spark NZ", "使用 Spark App 或登录 MySpark 查询",
		"https://www.spark.co.nz/help/account/manage/top-up-prepay", "当前官方页面未提供可审计的免费设备侧查询码"),
	ussdRule("sunrise_22802", "228", "002", "Sunrise Switzerland", "*121#", "CHF", costUnknown,
		"https://www.sunrise.ch/de/support/mobile/nutzung-und-einstellungen/handy-codes-ussd"),
	unsupportedRule("three_hk_454003", "454", "003", "Three Hong Kong", "部分预付卡可使用 ##107#，请按卡产品说明确认",
		"https://www.three.com.hk/download/ppsim/HSDPA_guide_e.pdf", "公开说明针对特定预付卡产品"),
	unsupportedRule("three_uk_234020", "234", "020", "Three UK", "使用 Three App 或 My3 查询",
		"https://www.three.co.uk/support/pay-as-you-go/managing-your-account-online", "当前官方页面仅提供 App/My3 查询"),
	ussdLimitedRule("tmobile_310240", "310", "240", "T-Mobile US", "#999#", "USD",
		"https://www.t-mobile.com/support/plans-features/self-service-short-codes/", "部分新套餐不支持该短码"),
	ussdLimitedRule("tmobile_310260", "310", "260", "T-Mobile US", "#999#", "USD",
		"https://www.t-mobile.com/support/plans-features/self-service-short-codes/", "部分新套餐不支持该短码"),
	smsRule("vodafone_nl_20404", "204", "04", "Vodafone Netherlands", "4000", "STATUS", "EUR", costUnknown,
		"https://www.vodafone.nl/abonnement/prepaid/en", []string{"4000", "Vodafone"}),
}

func BuiltInRules() []Rule {
	out := make([]Rule, len(builtInRules))
	for i := range builtInRules {
		out[i] = cloneRule(builtInRules[i])
	}
	return out
}

func FindBuiltIn(mcc, mnc string) (Rule, bool) {
	wanted, err := PLMNKey(mcc, mnc)
	if err != nil {
		return Rule{}, false
	}
	for _, rule := range builtInRules {
		key, _ := PLMNKey(rule.MCC, rule.MNC)
		if key == wanted {
			return cloneRule(rule), true
		}
	}
	return Rule{}, false
}

func cloneRule(rule Rule) Rule {
	rule.ExpectedSenders = append([]string(nil), rule.ExpectedSenders...)
	rule.Limitations = append([]string(nil), rule.Limitations...)
	return rule
}

func smsRule(id, mcc, mnc, operator, destination, payload, currency, cost, source string, senders []string) Rule {
	return Rule{ID: id, MCC: mcc, MNC: mnc, Operator: operator, Transport: TransportSMS, Destination: destination,
		Payload: payload, ResponseMode: ResponseSMS, ExpectedSenders: senders, ParserPattern: balancePattern,
		Currency: currency, CostStatus: cost, EvidenceType: official, EvidenceURL: source, Enabled: true, BuiltIn: true}
}

func smsProjectRule(id, mcc, mnc, operator, destination, payload, currency string, senders []string, note string) Rule {
	rule := smsRule(id, mcc, mnc, operator, destination, payload, currency, costUnknown, "", senders)
	rule.EvidenceType = projectTest
	rule.Limitations = []string{note}
	return rule
}

func ussdRule(id, mcc, mnc, operator, code, currency, cost, source string) Rule {
	return Rule{ID: id, MCC: mcc, MNC: mnc, Operator: operator, Transport: TransportUSSD, Payload: code,
		ResponseMode: ResponseDirect, ParserPattern: balancePattern, Currency: currency, CostStatus: cost,
		EvidenceType: official, EvidenceURL: source, Enabled: true, BuiltIn: true}
}

func ussdSMSRule(id, mcc, mnc, operator, code, currency, source string, senders []string, limitation string) Rule {
	rule := ussdRule(id, mcc, mnc, operator, code, currency, costUnknown, source)
	rule.ResponseMode = ResponseSMS
	rule.ExpectedSenders = senders
	rule.Limitations = []string{limitation}
	return rule
}

func ussdLimitedRule(id, mcc, mnc, operator, code, currency, source, limitation string) Rule {
	rule := ussdRule(id, mcc, mnc, operator, code, currency, costUnknown, source)
	rule.Limitations = []string{limitation}
	return rule
}

func unsupportedRule(id, mcc, mnc, operator, alternative, source, limitation string) Rule {
	return Rule{ID: id, MCC: mcc, MNC: mnc, Operator: operator, Transport: TransportUnsupported,
		ResponseMode: ResponseNone, CostStatus: costUnknown, EvidenceType: official, EvidenceURL: source,
		Limitations: []string{limitation}, Alternative: alternative, Enabled: true, BuiltIn: true}
}

const balancePattern = `(?i)(?:credit|balance|saldo|guthaben|tegoed)[^0-9]{0,32}(?P<amount>[0-9]+(?:[.,][0-9]{1,2})?)`
