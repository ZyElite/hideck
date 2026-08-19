package notify

import "github.com/yibaiba/hideck/internal/config"

type FeishuChannelOptions struct {
	Config     config.FeishuConfig
	StateStore RuntimeStateStore
}
