package ikev2

// PayloadType identifies an IKEv2 payload (RFC 7296 section 3.2).
type PayloadType uint8

const (
	NoNextPayload     PayloadType = 0
	SA                PayloadType = 33
	KE                PayloadType = 34
	IDi               PayloadType = 35
	IDr               PayloadType = 36
	CERT              PayloadType = 37
	CERTREQ           PayloadType = 38
	AUTH              PayloadType = 39
	NiNr              PayloadType = 40
	N                 PayloadType = 41
	D                 PayloadType = 42
	V                 PayloadType = 43
	TSI               PayloadType = 44
	TSR               PayloadType = 45
	SK                PayloadType = 46
	CP                PayloadType = 47
	EAP               PayloadType = 48
	EncryptedFragment PayloadType = 53
)

// Compatibility names retained from the reconstructed API.
const (
	PayloadNoNext    = 0
	PayloadSA        = 33
	PayloadKE        = 34
	PayloadIDi       = 35
	PayloadIDr       = 36
	PayloadCert      = 37
	PayloadCertReq   = 38
	PayloadAuth      = 39
	PayloadNi        = 40
	PayloadNotify    = 41
	PayloadDelete    = 42
	PayloadVendorID  = 43
	PayloadTS        = 44
	PayloadTSi       = 44
	PayloadTSr       = 45
	PayloadEncrypted = 46
	PayloadCP        = 47
	PayloadEAP       = 48
)

type ExchangeType uint8

const (
	IKE_SA_INIT        ExchangeType = 34
	IKE_AUTH           ExchangeType = 35
	CREATE_CHILD_SA    ExchangeType = 36
	INFORMATIONAL      ExchangeType = 37
	IKE_SESSION_RESUME ExchangeType = 38
)

const (
	ExchangeIKEInit       = 34
	ExchangeIKEAuth       = 35
	ExchangeCreateChildSA = 36
	ExchangeInformational = 37
)

type ProtocolID uint8

const (
	ProtoIKE ProtocolID = 1
	ProtoAH  ProtocolID = 2
	ProtoESP ProtocolID = 3
)

type TransformType uint8

const (
	TransformTypeEncr  TransformType = 1
	TransformTypePRF   TransformType = 2
	TransformTypeInteg TransformType = 3
	TransformTypeDH    TransformType = 4
	TransformTypeESN   TransformType = 5
)

const (
	TypeEncryption = 1
	TypePRF        = 2
	TypeIntegrity  = 3
	TypeDHGroup    = 4
	TypeESN        = 5
)

type AlgorithmType uint16

const (
	ENCR_DES_IV64   AlgorithmType = 1
	ENCR_DES        AlgorithmType = 2
	ENCR_3DES       AlgorithmType = 3
	ENCR_RC5        AlgorithmType = 4
	ENCR_IDEA       AlgorithmType = 5
	ENCR_CAST       AlgorithmType = 6
	ENCR_BLOWFISH   AlgorithmType = 7
	ENCR_3IDEA      AlgorithmType = 8
	ENCR_DES_IV32   AlgorithmType = 9
	ENCR_NULL       AlgorithmType = 11
	ENCR_AES_CBC    AlgorithmType = 12
	ENCR_AES_CTR    AlgorithmType = 13
	ENCR_AES_CCM_8  AlgorithmType = 14
	ENCR_AES_CCM_12 AlgorithmType = 15
	ENCR_AES_CCM_16 AlgorithmType = 16
	ENCR_AES_GCM_8  AlgorithmType = 18
	ENCR_AES_GCM_12 AlgorithmType = 19
	ENCR_AES_GCM_16 AlgorithmType = 20
)

const (
	PRF_HMAC_MD5      AlgorithmType = 1
	PRF_HMAC_SHA1     AlgorithmType = 2
	PRF_HMAC_TIGER    AlgorithmType = 3
	PRF_AES128_XCBC   AlgorithmType = 4
	PRF_HMAC_SHA2_256 AlgorithmType = 5
	PRF_HMAC_SHA2_384 AlgorithmType = 6
	PRF_HMAC_SHA2_512 AlgorithmType = 7
	PRF_AES128_CMAC   AlgorithmType = 8
)

const (
	AUTH_NONE              AlgorithmType = 0
	AUTH_HMAC_MD5_96       AlgorithmType = 1
	AUTH_HMAC_SHA1_96      AlgorithmType = 2
	AUTH_DES_MAC           AlgorithmType = 3
	AUTH_KPDK_MD5          AlgorithmType = 4
	AUTH_AES_XCBC_96       AlgorithmType = 5
	AUTH_HMAC_MD5_128      AlgorithmType = 6
	AUTH_HMAC_SHA1_160     AlgorithmType = 7
	AUTH_AES_CMAC_96       AlgorithmType = 8
	AUTH_AES_128_GMAC      AlgorithmType = 9
	AUTH_AES_192_GMAC      AlgorithmType = 10
	AUTH_AES_256_GMAC      AlgorithmType = 11
	AUTH_HMAC_SHA2_256_128 AlgorithmType = 12
	AUTH_HMAC_SHA2_384_192 AlgorithmType = 13
	AUTH_HMAC_SHA2_512_256 AlgorithmType = 14
)

const (
	MODP_768_bit  AlgorithmType = 1
	MODP_1024_bit AlgorithmType = 2
	MODP_1536_bit AlgorithmType = 5
	MODP_2048_bit AlgorithmType = 14
	MODP_3072_bit AlgorithmType = 15
	MODP_4096_bit AlgorithmType = 16
	MODP_6144_bit AlgorithmType = 17
	MODP_8192_bit AlgorithmType = 18
	ECP_256_bit   AlgorithmType = 19
	ECP_384_bit   AlgorithmType = 20
)

const AttributeKeyLength uint16 = 14

const (
	UNSUPPORTED_CRITICAL_PAYLOAD uint16 = 1
	INVALID_IKE_SPI              uint16 = 4
	INVALID_MAJOR_VERSION        uint16 = 5
	INVALID_SYNTAX               uint16 = 7
	INVALID_MESSAGE_ID           uint16 = 9
	INVALID_SPI                  uint16 = 11
	NO_PROPOSAL_CHOSEN           uint16 = 14
	INVALID_KE_PAYLOAD           uint16 = 17
	AUTHENTICATION_FAILED        uint16 = 24
	SINGLE_PAIR_REQUIRED         uint16 = 34
	NO_ADDITIONAL_SAS            uint16 = 35
	INTERNAL_ADDRESS_FAILURE     uint16 = 36
	FAILED_CP_REQUIRED           uint16 = 37
	TS_UNACCEPTABLE              uint16 = 38
	INVALID_SELECTORS            uint16 = 39
	UNACCEPTABLE_ADDRESSES       uint16 = 40
	UNEXPECTED_NAT_DETECTED      uint16 = 41
	USE_ASSIGNED_HoA             uint16 = 42
	TEMPORARY_FAILURE            uint16 = 43
	CHILD_SA_NOT_FOUND           uint16 = 44
	INVALID_GROUP_ID             uint16 = 45
	AUTHORIZATION_FAILED         uint16 = 46
	STATE_NOT_FOUND              uint16 = 47
	TS_MAX_QUEUE                 uint16 = 48
	REGISTRATION_FAILED          uint16 = 49
)

const (
	INITIAL_CONTACT                     uint16 = 16384
	SET_WINDOW_SIZE                     uint16 = 16385
	ADDITIONAL_TS_POSSIBLE              uint16 = 16386
	IPCOMP_SUPPORTED                    uint16 = 16387
	NAT_DETECTION_SOURCE_IP             uint16 = 16388
	NAT_DETECTION_DESTINATION_IP        uint16 = 16389
	COOKIE                              uint16 = 16390
	USE_TRANSPORT_MODE                  uint16 = 16391
	HTTP_CERT_LOOKUP_SUPPORTED          uint16 = 16392
	REKEY_SA                            uint16 = 16393
	ESP_TFC_PADDING_NOT_SUPPORTED       uint16 = 16394
	NON_FIRST_FRAGMENTS_ALSO            uint16 = 16395
	MOBIKE_SUPPORTED                    uint16 = 16396
	ADDITIONAL_IP4_ADDRESS              uint16 = 16397
	ADDITIONAL_IP6_ADDRESS              uint16 = 16398
	NO_ADDITIONAL_ADDRESSES             uint16 = 16399
	UPDATE_SA_ADDRESSES                 uint16 = 16400
	COOKIE2                             uint16 = 16401
	NO_NATS_ALLOWED                     uint16 = 16402
	AUTH_LIFETIME                       uint16 = 16403
	REDIRECT_SUPPORTED                  uint16 = 16406
	REDIRECT                            uint16 = 16407
	TICKET_LT_OPAQUE                    uint16 = 16409
	TICKET_REQUEST                      uint16 = 16410
	TICKET_ACK                          uint16 = 16411
	TICKET_NACK                         uint16 = 16412
	TICKET_OPAQUE                       uint16 = 16413
	EAP_ONLY_AUTHENTICATION             uint16 = 16417
	IKEV2_MESSAGE_ID_SYNC_SUPPORTED     uint16 = 16420
	IPSEC_REPLAY_COUNTER_SYNC_SUPPORTED uint16 = 16421
	IKEV2_MESSAGE_ID_SYNC               uint16 = 16422
	IPSEC_REPLAY_COUNTER_SYNC           uint16 = 16423
	SECURE_PASSWORD_METHODS             uint16 = 16424
	PSK_PERSIST                         uint16 = 16425
	PSK_CONFIRM                         uint16 = 16426
	ERX_SUPPORTED                       uint16 = 16427
	IFOM_CAPABILITY                     uint16 = 16428
	GROUP_SENDER                        uint16 = 16429
	IKEV2_FRAGMENTATION_SUPPORTED       uint16 = 16430
	SIGNATURE_HASH_ALGORITHMS           uint16 = 16431
	CLONE_IKE_SA_SUPPORTED              uint16 = 16432
	CLONE_IKE_SA                        uint16 = 16433
	PUZZLE                              uint16 = 16434
	USE_PPK                             uint16 = 16435
	PPK_IDENTITY                        uint16 = 16436
	NO_PPK_AUTH                         uint16 = 16437
	INTERMEDIATE_EXCHANGE_SUPPORTED     uint16 = 16438
	IP4_ALLOWED                         uint16 = 16439
	IP6_ALLOWED                         uint16 = 16440
	ADDITIONAL_KEY_EXCHANGE             uint16 = 16441
	USE_AGGFRAG                         uint16 = 16442
	SUPPORTED_AUTH_METHODS              uint16 = 16443
	SA_RESOURCE_INFO                    uint16 = 16444
	USE_PPK_INT                         uint16 = 16445
	PPK_IDENTITY_KEY                    uint16 = 16446
	DEVICE_IDENTITY                     uint16 = 16432
	DEVICE_IDENTITY_3GPP                uint16 = 41101
	N3GPP_GENERIC_ERROR                 uint16 = 40960
	N3GPP_NETWORK_FAILURE               uint16 = 41042
)

const (
	NotifyTypeEAPOnlyAuthentication = EAP_ONLY_AUTHENTICATION
	NotifyTypeRekeySA               = REKEY_SA
)

const (
	RedirectGWIPv4 uint8 = 1
	RedirectGWIPv6 uint8 = 2
	RedirectGWFQDN uint8 = 3
)
