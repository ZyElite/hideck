package profile

import (
	"crypto/sha256"
	"strconv"
	"strings"
)

const defaultUserAgent = "iOS/18.2.1 iPhone (iPhone15,4)"

type knownDeviceModel struct {
	canonical string
	tac       string
	aliases   []string
}

var knownDeviceModels = []knownDeviceModel{
	{"iphone15,4", "35022564", []string{"iphone15,4", "iphone 15"}},
	{"iphone15,2", "35022562", []string{"iphone15,2", "iphone 14 pro"}},
	{"iphone15,3", "35022563", []string{"iphone15,3", "iphone 14 pro max"}},
	{"iphone14,2", "35022552", []string{"iphone14,2", "iphone 13 pro"}},
	{"iphone14,3", "35022553", []string{"iphone14,3", "iphone 13 pro max"}},
	{"iphone16,1", "35022571", []string{"iphone16,1", "iphone 15 pro"}},
	{"iphone16,2", "35022572", []string{"iphone16,2", "iphone 15 pro max"}},
	{"galaxy_s24_ultra", "35819412", []string{"sm-s928", "galaxy s24 ultra"}},
	{"galaxy_s24_plus", "35819411", []string{"sm-s926", "galaxy s24+"}},
	{"galaxy_s24", "35819410", []string{"sm-s921", "galaxy s24"}},
	{"galaxy_s23_ultra", "35819392", []string{"sm-s918", "galaxy s23 ultra"}},
	{"galaxy_s23", "35819390", []string{"sm-s911", "galaxy s23"}},
	{"galaxy_s22", "35819370", []string{"sm-s901", "galaxy s22"}},
	{"pixel_8_pro", "35101288", []string{"pixel 8 pro", "husky"}},
	{"pixel_8", "35101287", []string{"pixel 8", "shiba"}},
	{"pixel_7_pro", "35101277", []string{"pixel 7 pro", "cheetah"}},
	{"xiaomi_14", "86699141", []string{"xiaomi 14", "23127pn0cc"}},
	{"xiaomi_13", "86699131", []string{"xiaomi 13", "2211133c"}},
	{"redmi_note_13_pro", "86712231", []string{"redmi note 13 pro", "2312drafd"}},
	{"oppo_find_x7", "86745571", []string{"find x7", "phz110"}},
	{"oneplus_12", "86392121", []string{"oneplus 12", "pjz110"}},
	{"rmx3366", "86034905", []string{"rmx3366", "realme rmx3366"}},
	{"vivo_x100", "86851301", []string{"vivo x100", "v2309a"}},
	{"huawei_p60_pro", "86147861", []string{"huawei p60 pro", "mna-al00"}},
}

var userAgents = map[string]string{
	"iphone14,2":        "iOS/18.2.1 iPhone (iPhone14,2)",
	"iphone14,3":        "iOS/18.2.1 iPhone (iPhone14,3)",
	"iphone15,2":        "iOS/18.2.1 iPhone (iPhone15,2)",
	"iphone15,3":        "iOS/18.2.1 iPhone (iPhone15,3)",
	"iphone15,4":        defaultUserAgent,
	"iphone16,1":        "iOS/18.2.1 iPhone (iPhone16,1)",
	"iphone16,2":        "iOS/18.2.1 iPhone (iPhone16,2)",
	"galaxy_s22":        "samsung_SM-S901B_13.0.0",
	"galaxy_s23":        "samsung_SM-S911B_14.0.0",
	"galaxy_s23_ultra":  "samsung_SM-S918B_14.0.0",
	"galaxy_s24":        "samsung_SM-S921B_14.0.0",
	"galaxy_s24_plus":   "samsung_SM-S926B_14.0.0",
	"galaxy_s24_ultra":  "samsung_SM-S928B_14.0.0",
	"pixel_7_pro":       "google_Pixel7Pro_14.0.0",
	"pixel_8":           "google_Pixel8_14.0.0",
	"pixel_8_pro":       "google_Pixel8Pro_14.0.0",
	"xiaomi_13":         "xiaomi_13_14.0.0",
	"xiaomi_14":         "xiaomi_14_14.0.0",
	"redmi_note_13_pro": "redmi_Note13Pro_14.0.0",
	"oppo_find_x7":      "oppo_FindX7_14.0.0",
	"oneplus_12":        "oneplus_12_14.0.0",
	"rmx3366":           "realme_RMX3366_0.0.2100",
	"vivo_x100":         "vivo_X100_14.0.0",
	"huawei_p60_pro":    "huawei_P60Pro_13.0.0",
}

func ResolveUserAgentForModel(model string) string {
	if value := userAgents[normalizeModelName(model)]; value != "" {
		return value
	}
	return defaultUserAgent
}

func GenerateStableIMEIForModel(seed, model string) string {
	canonical, tac, ok := resolveKnownModelTAC(model)
	if !ok || tac == "" {
		return ""
	}
	seed = strings.TrimSpace(seed)
	if seed == "" {
		seed = canonical
	}
	digest := sha256.Sum256([]byte(seed + "|" + canonical + "|" + tac))
	prefix := tac + stableDigitsN(digest[:], 6)
	return prefix + imeiLuhnCheckDigit(prefix)
}

func stableDigitsN(digest []byte, count int) string {
	if count > len(digest) {
		count = len(digest)
	}
	var result strings.Builder
	result.Grow(count)
	for _, value := range digest[:count] {
		result.WriteByte('0' + value%10)
	}
	return result.String()
}

func normalizeModelName(model string) string {
	value := strings.ToLower(strings.TrimSpace(model))
	for _, known := range knownDeviceModels {
		if value == known.canonical {
			return known.canonical
		}
	}
	return ""
}

func resolveKnownModelTAC(model string) (string, string, bool) {
	value := strings.ToLower(strings.TrimSpace(model))
	for _, known := range knownDeviceModels {
		if value == known.canonical {
			return known.canonical, known.tac, true
		}
	}
	for _, known := range knownDeviceModels {
		for _, alias := range known.aliases {
			if strings.Contains(value, alias) {
				return known.canonical, known.tac, true
			}
		}
	}
	return "", "", false
}

func imeiLuhnCheckDigit(prefix string) string {
	if len(prefix) != 14 {
		return ""
	}
	sum := 0
	double := true
	for index := len(prefix) - 1; index >= 0; index-- {
		digit := int(prefix[index] - '0')
		if digit < 0 || digit > 9 {
			return ""
		}
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}
	return strconv.Itoa((10 - sum%10) % 10)
}
