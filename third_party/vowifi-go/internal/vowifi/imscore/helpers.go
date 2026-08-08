package imscore

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

// SIPStatusText returns the standard reason phrase for a SIP status code
// (RFC 3261 §21).
func SIPStatusText(code int) string {
	switch {
	case code >= 100 && code < 200:
		switch code {
		case 100:
			return "Trying"
		case 180:
			return "Ringing"
		case 181:
			return "Call Is Being Forwarded"
		case 182:
			return "Queued"
		case 183:
			return "Session Progress"
		default:
			return "Informational"
		}
	case code >= 200 && code < 300:
		if code == 200 {
			return "OK"
		}
		return "Successful"
	case code >= 300 && code < 400:
		switch code {
		case 300:
			return "Multiple Choices"
		case 301:
			return "Moved Permanently"
		case 302:
			return "Moved Temporarily"
		case 305:
			return "Use Proxy"
		case 380:
			return "Alternative Service"
		default:
			return "Redirection"
		}
	case code >= 400 && code < 500:
		switch code {
		case 400:
			return "Bad Request"
		case 401:
			return "Unauthorized"
		case 402:
			return "Payment Required"
		case 403:
			return "Forbidden"
		case 404:
			return "Not Found"
		case 405:
			return "Method Not Allowed"
		case 406:
			return "Not Acceptable"
		case 407:
			return "Proxy Authentication Required"
		case 408:
			return "Request Timeout"
		case 409:
			return "Conflict"
		case 410:
			return "Gone"
		case 413:
			return "Request Entity Too Large"
		case 414:
			return "Request-URI Too Long"
		case 415:
			return "Unsupported Media Type"
		case 416:
			return "Unsupported URI Scheme"
		case 420:
			return "Bad Extension"
		case 421:
			return "Extension Required"
		case 423:
			return "Interval Too Brief"
		case 480:
			return "Temporarily Unavailable"
		case 481:
			return "Call/Transaction Does Not Exist"
		case 482:
			return "Loop Detected"
		case 483:
			return "Too Many Hops"
		case 484:
			return "Address Incomplete"
		case 485:
			return "Ambiguous"
		case 486:
			return "Busy Here"
		case 487:
			return "Request Terminated"
		case 488:
			return "Not Acceptable Here"
		case 491:
			return "Request Pending"
		case 493:
			return "Undecipherable"
		default:
			return "Client Error"
		}
	default:
		switch code {
		case 500:
			return "Server Internal Error"
		case 501:
			return "Not Implemented"
		case 502:
			return "Bad Gateway"
		case 503:
			return "Service Unavailable"
		case 504:
			return "Server Time-out"
		case 505:
			return "Version Not Supported"
		case 513:
			return "Message Too Large"
		case 600:
			return "Busy Everywhere"
		case 603:
			return "Decline"
		case 604:
			return "Does Not Exist Anywhere"
		case 606:
			return "Not Acceptable"
		default:
			return fmt.Sprintf("Status %d", code)
		}
	}
}

// CountryISO2FromMCC returns the ISO 3166-1 alpha-2 country code for an MCC.
func CountryISO2FromMCC(mcc string) string {
	switch mcc {
	case "310", "311", "312", "313", "314", "315", "316", "317", "318":
		return "US"
	case "302":
		return "CA"
	case "334":
		return "MX"
	case "460":
		return "CN"
	case "440", "441":
		return "JP"
	case "450":
		return "KR"
	case "208":
		return "FR"
	case "262":
		return "DE"
	case "234", "235":
		return "GB"
	case "222":
		return "IT"
	case "214":
		return "ES"
	case "204":
		return "NL"
	case "228":
		return "CH"
	case "232":
		return "AT"
	case "206":
		return "BE"
	case "505":
		return "AU"
	case "530":
		return "NZ"
	case "404", "405", "406":
		return "IN"
	case "452":
		return "VN"
	case "520":
		return "TH"
	case "502":
		return "MY"
	case "525":
		return "SG"
	case "510":
		return "ID"
	case "515":
		return "PH"
	case "455":
		return "MO"
	case "466":
		return "TW"
	case "901":
		return "UNK"
	default:
		return "XX"
	}
}

// IsFatalNetworkError reports whether an error indicates a fatal network
// failure that should stop the session (rather than a transient failure).
func IsFatalNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout() == false // network errors that are not timeouts are fatal
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no route to host"),
		strings.Contains(msg, "network is unreachable"),
		strings.Contains(msg, "permission denied"),
		strings.Contains(msg, "econnrefused"),
		strings.Contains(msg, "ehostunreach"),
		strings.Contains(msg, "enetunreach"):
		return true
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "temporarily unavailable"),
		strings.Contains(msg, "i/o timeout"):
		return false
	default:
		return false
	}
}

// GenerateStablePAccessNetworkInfo builds the WLAN P-Access-Network-Info
// value used by the recovered implementation.
func GenerateStablePAccessNetworkInfo(seed string) string {
	return fmt.Sprintf(`IEEE-802.11; i-wlan-node-id="%s"`, GenerateStableWlanNodeID(seed))
}

// GenerateStablePAccessNetworkInfoByIdentity builds PANI from an identity.
func GenerateStablePAccessNetworkInfoByIdentity(ident identity.IMSIdentity) string {
	seed := stablePANIGenerationSeed([]string{
		ident.IMPI,
		ident.IMPU,
		ident.Domain,
		string(ident.ActualSource),
	})
	return GenerateStablePAccessNetworkInfo(seed)
}

// AppendPAccessNetworkCountry appends a country to the PANI value.
func AppendPAccessNetworkCountry(pani, iso2 string) string {
	iso2 = strings.ToUpper(strings.TrimSpace(iso2))
	if iso2 == "" || strings.Contains(strings.ToLower(pani), "country=") {
		return pani
	}
	return pani + ";country=" + iso2
}

// GenerateStableWlanNodeID derives a stable WLAN node ID from an identity.
func GenerateStableWlanNodeID(seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(seed))
	// Present the stable value as a locally administered unicast MAC address.
	digest[0] = digest[0]&^byte(1) | byte(2)
	return fmt.Sprintf("%x", digest[:6])
}

func stablePANIGenerationSeed(candidates []string) string {
	for _, candidate := range candidates {
		if seed := strings.TrimSpace(candidate); seed != "" {
			return seed
		}
	}
	return ""
}

// GenerateRandomIMEIForModel generates a random IMEI for a device model.
func GenerateRandomIMEIForModel(model string) string {
	tac := "35693803"
	if strings.EqualFold(strings.TrimSpace(model), "iphone15,4") {
		tac = "86034905"
	}
	random := make([]byte, 6)
	_, _ = rand.Read(random)
	serial := make([]byte, len(random))
	for index, value := range random {
		serial[index] = '0' + value%10
	}
	prefix := tac + string(serial)
	return prefix + string(imeiCheckDigit(prefix))
}

// GenerateDefaultCellularNetworkInfo builds a default cellular network info
// string from the SIM profile.
func GenerateDefaultCellularNetworkInfo(mcc, mnc string) string {
	mcc = strings.TrimSpace(mcc)
	mncValue, err := strconv.Atoi(strings.TrimSpace(mnc))
	if err != nil || len(mcc) != 3 || mncValue < 0 || mncValue > 999 {
		return ""
	}
	cellID := fmt.Sprintf("%s%03d0%s", mcc, mncValue, strings.ToUpper(randomHex(9)))
	ageBytes := make([]byte, 2)
	_, _ = rand.Read(ageBytes)
	age := 1000 + (int(ageBytes[0]) << 8) + int(ageBytes[1])
	return fmt.Sprintf("3GPP-E-UTRAN-TDD;utran-cell-id-3gpp=%s;cell-info-age=%d", cellID, age)
}

func imeiCheckDigit(prefix string) byte {
	sum := 0
	for index, char := range prefix {
		digit := int(char - '0')
		if index%2 == 1 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return byte('0' + (10-sum%10)%10)
}

// ResolveIMSIdentitySource resolves the IMS identity source preference.
func ResolveIMSIdentitySource(pref string, hasISIM, hasUSIM bool) string {
	switch strings.ToLower(strings.TrimSpace(pref)) {
	case "usim":
		return "usim"
	case "imei":
		return "imei"
	case "isim":
		fallthrough
	default:
		if hasISIM {
			return "isim"
		}
		if hasUSIM {
			return "usim"
		}
		return "imei"
	}
}

// LogEPDGSnapshot logs an ePDG selection snapshot.
func LogEPDGSnapshot(deviceID, epdg, source string) {
	logging.Info("ePDG selection", "device", deviceID, "epdg", epdg, "source", source)
}

// mccMncFromIdentity derives MCC/MNC from an IMSI in an identity.
func mccMncFromIdentity(ident identity.IMSIdentity) (mcc, mnc string) {
	imsi := ident.IMPI
	if i := strings.IndexByte(imsi, '@'); i > 0 {
		imsi = imsi[:i]
	}
	if len(imsi) >= 5 {
		mcc = imsi[:3]
		mnc = imsi[3:5]
	}
	if len(mnc) == 2 {
		mnc = "0" + mnc
	}
	return mcc, mnc
}

// BuildIMSConfigFromCarrier builds an IMSConfig from a carrier prepared
// session.
func BuildIMSConfigFromCarrier(deviceID string, ident identity.IMSIdentity, epdgAddr string) *IMSConfig {
	mcc, mnc := mccMncFromIdentity(ident)
	domain := ident.Domain
	if domain == "" {
		domain = fmt.Sprintf("ims.mnc%s.mcc%s.3gppnetwork.org", mnc, mcc)
	}
	impi := ident.IMPI
	if impi == "" {
		impi = deviceID + "@" + domain
	}
	impu := []string{ident.IMPU}
	if len(impu) == 0 || impu[0] == "" {
		impu = []string{"sip:" + impi}
	}
	return &IMSConfig{
		DeviceID:  deviceID,
		IMSI:      mcc + mnc + "0000000",
		IMPI:      impi,
		IMPU:      impu,
		Domain:    domain,
		Realm:     domain,
		EPDGAddr:  epdgAddr,
		Transport: "udp",
		Expires:   3600 * time.Second,
		TraceID:   newTraceID(),
	}
}

// ApplyResolvedIMSIdentityToConfig applies a resolved IMS identity to an
// existing config.
func ApplyResolvedIMSIdentityToConfig(cfg *IMSConfig, ident identity.IMSIdentity) {
	if cfg == nil {
		return
	}
	if ident.IMPI != "" {
		cfg.IMPI = ident.IMPI
	}
	if ident.IMPU != "" {
		cfg.IMPU = []string{ident.IMPU}
	}
	if ident.Domain != "" {
		cfg.Domain = ident.Domain
	}
	if cfg.Realm == "" {
		cfg.Realm = cfg.Domain
	}
	if cfg.IMSI == "" {
		imsi := ident.IMPI
		if i := strings.IndexByte(imsi, '@'); i > 0 {
			imsi = imsi[:i]
		}
		cfg.IMSI = imsi
	}
}

// SetupService wires the IMS service surface for a device.
func SetupService(deviceID string, cfg *IMSConfig) (*Service, error) {
	return New(cfg)
}

// StartSessionIMSCore starts the IMS core session for a prepared identity.
func StartSessionIMSCore(deviceID string, ident identity.IMSIdentity, epdgAddr string) (*Service, error) {
	cfg := BuildIMSConfigFromCarrier(deviceID, ident, epdgAddr)
	return New(cfg)
}

// newTraceID returns a random trace id.
func newTraceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
