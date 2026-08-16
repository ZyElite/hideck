package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

const (
	ESIMTransportAT             = "at"
	ESIMTransportQMI            = "qmi"
	ESIMTransportMBIM           = "mbim"
	ESIMTransportPCSC           = "pcsc"
	MBIMTransportAuto           = "auto"
	MBIMTransportProxy          = "proxy"
	MBIMTransportDirect         = "direct"
	DefaultWebhookTextTemplate  = "{{device_label}} {{text}}"
	TelegramRecordingModeVoice  = "voice"
	TelegramRecordingModeAudio  = "audio"
	DefaultWeComPayloadTemplate = `{
  "msgtype": "text",
  "text": {
    "content": {{message}}
  }
}`
	WebPasswordEnvironmentVariable = "PROXY_WEB_PASSWORD"
)

type WebPasswordSource string

const (
	WebPasswordSourceDefault     WebPasswordSource = "default"
	WebPasswordSourceConfigFile  WebPasswordSource = "config_file"
	WebPasswordSourceEnvironment WebPasswordSource = "environment"
)

func NormalizeESIMTransport(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", ESIMTransportAT:
		return ESIMTransportAT
	case ESIMTransportQMI:
		return ESIMTransportQMI
	case ESIMTransportMBIM:
		return ESIMTransportMBIM
	case ESIMTransportPCSC:
		return ESIMTransportPCSC
	default:
		return strings.ToLower(strings.TrimSpace(in))
	}
}

func ValidateESIMTransport(in string) error {
	switch NormalizeESIMTransport(in) {
	case ESIMTransportAT, ESIMTransportQMI, ESIMTransportMBIM, ESIMTransportPCSC:
		return nil
	default:
		return fmt.Errorf("invalid esim transport: %q", strings.TrimSpace(in))
	}
}

func NormalizeTelegramRecordingMode(in string) string {
	mode := strings.ToLower(strings.TrimSpace(in))
	if mode == "" {
		return TelegramRecordingModeVoice
	}
	return mode
}

func ValidateTelegramRecordingMode(in string) error {
	switch NormalizeTelegramRecordingMode(in) {
	case TelegramRecordingModeVoice, TelegramRecordingModeAudio:
		return nil
	default:
		return fmt.Errorf("无效的 Telegram 录音发送样式: %q (允许 voice|audio)", strings.TrimSpace(in))
	}
}

func NormalizeMBIMTransport(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", MBIMTransportAuto:
		return MBIMTransportAuto
	case MBIMTransportProxy:
		return MBIMTransportProxy
	case MBIMTransportDirect:
		return MBIMTransportDirect
	default:
		return MBIMTransportAuto
	}
}

// ResolveIPFamily parses DeviceConfig.IPVersion into IPv4/IPv6 enable flags.
// Empty input preserves the legacy IPv4-only behavior.
func ResolveIPFamily(in string) (enableV4 bool, enableV6 bool, err error) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", "v4", "ipv4":
		return true, false, nil
	case "v6", "ipv6":
		return false, true, nil
	case "v4v6", "v6v4", "dual", "ipv4v6":
		return true, true, nil
	default:
		return false, false, fmt.Errorf("无效的 ip_version: %q (允许 v4|v6|v4v6)", in)
	}
}

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	System   SystemConfig   `mapstructure:"system"`
	Devices  []DeviceConfig `mapstructure:"devices"`
	Telegram TelegramConfig `mapstructure:"telegram"`
	Feishu   FeishuConfig   `mapstructure:"feishu"`
	QQ       QQConfig       `mapstructure:"qq"`
	Weixin   WeixinConfig   `mapstructure:"weixin"`
	WeComBot WeComBotConfig `mapstructure:"wecom_bot"`
	Webhook  WebhookConfig  `mapstructure:"webhook"`

	Bark     BarkConfig     `mapstructure:"bark"`
	Email    EmailConfig    `mapstructure:"email"`
	Pushplus PushplusConfig `mapstructure:"pushplus"`
	WeCom    WeComConfig    `mapstructure:"wecom"`
	Web      WebConfig      `mapstructure:"web"`
	Proxy    ProxyConfig    `mapstructure:"proxy"`
	VoWiFi   VoWiFiConfig   `mapstructure:"vowifi"`
}

type SystemConfig struct {
	OpenWRTDynamicInterfaces bool `mapstructure:"openwrt_dynamic_interfaces" json:"openwrt_dynamic_interfaces"`
}

// ProxyConfig 定义代理服务配置
type ProxyConfig struct {
	Instances []ProxyInstance `mapstructure:"instances"` // 代理实例列表
}

type VoWiFiConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	DeviceID string `mapstructure:"device_id"` // 留空则取第一个
	Mode     string `mapstructure:"mode"`      // vowifi|volte(当前会回退为 vowifi)，默认 vowifi
}

// ProxyInstance 定义一个代理实例配置
type ProxyInstance struct {
	ID          string `mapstructure:"id" json:"id"`                   // 实例唯一标识
	Name        string `mapstructure:"name" json:"name"`               // 显示名称
	DeviceID    string `mapstructure:"device_id" json:"device_id"`     // 绑定设备 ID（强制绑定对应网卡）
	Enabled     bool   `mapstructure:"enabled" json:"enabled"`         // 是否启用
	Mode        string `mapstructure:"mode" json:"mode"`               // 代理模式: socks5|http
	ListenAddr  string `mapstructure:"listen_addr" json:"listen_addr"` // 监听地址
	ListenPort  int    `mapstructure:"listen_port" json:"listen_port"` // 监听端口
	AuthEnabled bool   `mapstructure:"auth_enabled" json:"auth_enabled"`
	Username    string `mapstructure:"username" json:"username"`
	Password    string `mapstructure:"password" json:"password"`
}

type WebConfig struct {
	Username       string            `mapstructure:"username"`
	Password       string            `mapstructure:"password"`
	PasswordSource WebPasswordSource `mapstructure:"-"`
}

type ServerConfig struct {
	Port                 string   `mapstructure:"port"`
	HTTPSEnabled         bool     `mapstructure:"https_enabled"`
	HTTPSPort            string   `mapstructure:"https_port"`
	WebRTCUDPAddress     string   `mapstructure:"webrtc_udp_address"`
	WebRTCPublicHost     string   `mapstructure:"webrtc_public_host"`
	TLSCertFile          string   `mapstructure:"tls_cert_file"`
	TLSKeyFile           string   `mapstructure:"tls_key_file"`
	TLSDataDir           string   `mapstructure:"tls_data_dir"`
	ICEServers           []string `mapstructure:"ice_servers"`
	Debug                bool     `mapstructure:"debug"`
	SMSRateLimitDisabled bool     `mapstructure:"sms_rate_limit_disabled"`
}

type ESIMSwitchConfig struct {
	// UseRefreshTrue uses refresh=true for the main switch path. Default false preserves current behavior.
	UseRefreshTrue bool `mapstructure:"use_refresh_true"`
	// EventGatedConverge uses UIM indication events to gate post-switch convergence. Default false.
	EventGatedConverge bool `mapstructure:"event_gated_converge"`
	// RadioCycle performs LowPower -> Online radio cycling around switch. Default false.
	RadioCycle bool `mapstructure:"radio_cycle"`
	// ReinitWindowMS is the expected UIM reinitialization window in milliseconds. Default 0 disables the window.
	// Only effective when EventGatedConverge=true; ReinitWindow marks the period during which GetUIMReadiness
	// timeouts do not trigger whole-core recovery (to avoid triggering on firmware reinitialization stalls).
	// If EventGatedConverge=false, ReinitWindowMS is silently ignored.
	ReinitWindowMS int `mapstructure:"reinit_window_ms"`
	// NASAttachTimeoutMS bounds optional attach waiting after Online in milliseconds. Default 0 means do not block.
	NASAttachTimeoutMS int `mapstructure:"nas_attach_timeout_ms"`
}

type DeviceConfig struct {
	ID            string `mapstructure:"id"`
	Name          string `mapstructure:"name"` // 设备显示名称
	ModemIMEI     string `mapstructure:"modem_imei"`
	USBPath       string `mapstructure:"-"` // Deprecated: 运行时按 IMEI 现解析,绝不从文件读取
	ATPort        string `mapstructure:"-"` // Deprecated: 运行时解析;AT 终端用 Worker.ResolvedATPort()
	ProxyPort     int    `mapstructure:"proxy_port"`
	ManagePort    string `mapstructure:"-"`              // Deprecated: 运行时解析,绝不从文件读取
	Interface     string `mapstructure:"-"`              // Deprecated: 运行时解析,绝不从文件读取
	QMIDevice     string `mapstructure:"-"`              // Deprecated: 运行时解析,绝不从文件读取
	ControlDevice string `mapstructure:"-"`              // Deprecated: 运行时按 IMEI 现解析,绝不从文件读取
	MBIMTransport string `mapstructure:"mbim_transport"` // MBIM 传输: auto|proxy|direct，默认 auto
	QMIUseProxy   bool   `mapstructure:"qmi_use_proxy"`  // 是否通过 libqmi qmi-proxy 打开 QMI 控制口
	// 可选：qmi-proxy abstract socket 名称和可执行文件路径。留空使用 quectel-qmi-go 默认值。
	QMIProxyPath       string `mapstructure:"qmi_proxy_path"`
	QMIProxyExecutable string `mapstructure:"qmi_proxy_executable"`
	ESIMTransport      string `mapstructure:"esim_transport"` // eSIM 传输通道: at|qmi|mbim|pcsc，默认 at
	DeviceBackend      string `mapstructure:"device_backend"` // 设备后端模式: at|qmi|mbim|pcsc，默认 at
	PCSCReaderName     string `mapstructure:"pcsc_reader_name"`
	PCSCUSBPath        string `mapstructure:"pcsc_usb_path"`
	SIMPINEnv          string `mapstructure:"sim_pin_env"`
	USBNetMode         *int   `mapstructure:"usbnet_mode"` // 可选：用于校验/设置 Quectel USBNET 模式
	// ESIMSwitch controls deterministic eSIM switch behavior. Zero values preserve current behavior.
	ESIMSwitch ESIMSwitchConfig `mapstructure:"esim_switch"`

	OperatorSelectionMode string `mapstructure:"operator_selection_mode"`
	OperatorSelectionPLMN string `mapstructure:"operator_selection_plmn"`
	OperatorSelectionRAT  string `mapstructure:"operator_selection_rat"`

	// Serial config
	BaudRate int    `mapstructure:"baud_rate"`
	DataBits int    `mapstructure:"data_bits"`
	StopBits int    `mapstructure:"stop_bits"`
	Parity   string `mapstructure:"parity"`

	// 以下为运行时有效策略（投影自 card_policies，按 ICCID），不再从配置文件加载
	APN             string `mapstructure:"-"`
	NetworkEnabled  bool   `mapstructure:"-"`
	IPVersion       string `mapstructure:"-"`
	VoWiFiEnabled   bool   `mapstructure:"-"`
	AirplaneEnabled bool   `mapstructure:"-"`
	SMSEnabled      bool   `mapstructure:"-"` // SMS 恒开，运行时强制 true
	PhoneMode       string `mapstructure:"-"` // wifi | cellular
	DataStrategy    string `mapstructure:"-"` // always | on_demand

	// USB Audio (自动发现，无需手动配置)
	AudioDevice string `mapstructure:"-"` // Deprecated: 运行时解析,绝不从文件读取
}

type TelegramConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	BotToken      string `mapstructure:"bot_token"`
	ChatID        int64  `mapstructure:"chat_id"`
	AdminID       int64  `mapstructure:"admin_id"`
	RecordingMode string `mapstructure:"recording_mode"`
	BaseURL       string `mapstructure:"base_url"` // 反向代理地址 (例如 https://api.telegram.org/bot%s/%s)
	Proxy         string `mapstructure:"proxy"`    // HTTP 代理地址 (例如 http://127.0.0.1:7890)
}

// FeishuConfig 飞书通知配置
type FeishuConfig struct {
	Enabled   bool     `mapstructure:"enabled"`
	AppID     string   `mapstructure:"app_id"`     // 飞书开放平台应用 App ID
	AppSecret string   `mapstructure:"app_secret"` // 飞书开放平台应用 App Secret
	ChatIDs   []string `mapstructure:"chat_ids"`   // 飞书群聊 chat_id 列表
	ChatID    string   `mapstructure:"chat_id"`    // 兼容旧配置：单个 chat_id
}

type QQConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	AppID     string `mapstructure:"app_id"`
	AppSecret string `mapstructure:"app_secret"`
	GroupIDs  string `mapstructure:"group_ids"`  // 逗号分隔的群组 OpenID
	DirectIDs string `mapstructure:"direct_ids"` // 逗号分隔的私聊 OpenID
}

type WeixinConfig struct {
	Enabled         bool     `mapstructure:"enabled"`
	BaseURL         string   `mapstructure:"base_url"`
	CDNBaseURL      string   `mapstructure:"cdn_base_url"`
	AllowedUserIDs  []string `mapstructure:"allowed_user_ids"`
	AllowedGroupIDs []string `mapstructure:"allowed_group_ids"`
}

type WeComBotConfig struct {
	Enabled         bool     `mapstructure:"enabled"`
	BotID           string   `mapstructure:"bot_id"`
	Secret          string   `mapstructure:"secret"`
	WebSocketURL    string   `mapstructure:"websocket_url"`
	AllowedUserIDs  []string `mapstructure:"allowed_user_ids"`
	AllowedGroupIDs []string `mapstructure:"allowed_group_ids"`
}

type WebhookConfig struct {
	Enabled      bool              `mapstructure:"enabled"`
	URLs         []string          `mapstructure:"urls"`
	Secret       string            `mapstructure:"secret"`
	TimeoutMs    int               `mapstructure:"timeout_ms"`
	RetryMax     int               `mapstructure:"retry_max"`
	TextTemplate string            `mapstructure:"text_template"`
	Headers      map[string]string `mapstructure:"headers,omitempty" json:"headers,omitempty"`
}

type BarkConfig struct {
	Enabled bool     `mapstructure:"enabled"`
	URLs    []string `mapstructure:"urls"`
	Group   string   `mapstructure:"group"`
	Icon    string   `mapstructure:"icon"`
	Level   string   `mapstructure:"level"`
}

type EmailConfig struct {
	Enabled     bool     `mapstructure:"enabled"`
	UseSSL      bool     `mapstructure:"use_ssl"`
	SMTPHost    string   `mapstructure:"smtp_host"`
	SMTPPort    int      `mapstructure:"smtp_port"`
	Username    string   `mapstructure:"username"`
	Password    string   `mapstructure:"password"`
	FromAddress string   `mapstructure:"from_address"`
	ToAddresses []string `mapstructure:"to_addresses"`
}

type PushplusConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Token   string `mapstructure:"token"`
	Topic   string `mapstructure:"topic"`
	Channel string `mapstructure:"channel"`
}

type WeComConfig struct {
	Enabled         bool     `mapstructure:"enabled"`
	URLs            []string `mapstructure:"urls"`
	PayloadTemplate string   `mapstructure:"payload_template"`
}

type NotificationConfigs struct {
	Telegram TelegramConfig
	Feishu   FeishuConfig
	QQ       QQConfig
	Weixin   WeixinConfig
	WeComBot WeComBotConfig
	Webhook  WebhookConfig
	Bark     BarkConfig
	Email    EmailConfig
	Pushplus PushplusConfig
	WeCom    WeComConfig
}

func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	// 默认值设置
	viper.SetDefault("server.port", 7575)
	viper.SetDefault("system.openwrt_dynamic_interfaces", false)
	viper.SetDefault("telegram.recording_mode", TelegramRecordingModeVoice)
	viper.SetDefault("webhook.timeout_ms", 5000)
	viper.SetDefault("webhook.retry_max", 3)
	viper.SetDefault("webhook.text_template", DefaultWebhookTextTemplate)

	viper.SetDefault("bark.enabled", false)
	viper.SetDefault("bark.group", "hideck")
	viper.SetDefault("bark.level", "active")
	viper.SetDefault("email.enabled", false)
	viper.SetDefault("email.use_ssl", false)
	viper.SetDefault("pushplus.enabled", false)
	viper.SetDefault("wecom.enabled", false)
	viper.SetDefault("wecom.payload_template", DefaultWeComPayloadTemplate)
	viper.SetDefault("weixin.enabled", false)
	viper.SetDefault("weixin.base_url", "https://ilinkai.weixin.qq.com")
	viper.SetDefault("weixin.cdn_base_url", "https://novac2c.cdn.weixin.qq.com/c2c")
	viper.SetDefault("wecom_bot.enabled", false)
	viper.SetDefault("wecom_bot.websocket_url", "wss://openws.work.weixin.qq.com")
	viper.SetDefault("web.username", "admin")
	viper.SetDefault("web.password", "admin")
	// Missing values belong to legacy configurations, which always started HTTPS.
	// New installations explicitly set this to false in their generated template.
	viper.SetDefault("server.https_enabled", true)
	viper.SetDefault("server.https_port", 7576)
	viper.SetDefault("server.webrtc_udp_address", ":7580")
	viper.SetDefault("server.webrtc_public_host", "")
	viper.SetDefault("server.tls_data_dir", "data/tls")
	viper.SetDefault("vowifi.enabled", false)
	viper.SetDefault("vowifi.mode", "vowifi")
	viper.SetDefault("imscore.use_sipgo_udp", false)

	// 环境变量覆盖支持 (例如 PROXY_DEVICES_0_APN)
	viper.SetEnvPrefix("PROXY")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	cfg.Telegram.RecordingMode = NormalizeTelegramRecordingMode(cfg.Telegram.RecordingMode)
	if err := ValidateTelegramRecordingMode(cfg.Telegram.RecordingMode); err != nil {
		return nil, err
	}
	cfg.Web.PasswordSource = resolveWebPasswordSource()

	// 兼容旧版单值配置: feishu.chat_id
	if len(cfg.Feishu.ChatIDs) == 0 && strings.TrimSpace(cfg.Feishu.ChatID) != "" {
		cfg.Feishu.ChatIDs = []string{strings.TrimSpace(cfg.Feishu.ChatID)}
	}

	// 兼容 server.port 格式 (例如: 7575 和 :7575)
	if cfg.Server.Port != "" && !strings.Contains(cfg.Server.Port, ":") {
		cfg.Server.Port = ":" + cfg.Server.Port
	}
	if cfg.Server.HTTPSPort != "" && !strings.Contains(cfg.Server.HTTPSPort, ":") {
		cfg.Server.HTTPSPort = ":" + cfg.Server.HTTPSPort
	}
	return &cfg, nil
}

func resolveWebPasswordSource() WebPasswordSource {
	if value, exists := os.LookupEnv(WebPasswordEnvironmentVariable); exists && value != "" {
		return WebPasswordSourceEnvironment
	}
	if viper.InConfig("web.password") {
		return WebPasswordSourceConfigFile
	}
	return WebPasswordSourceDefault
}
