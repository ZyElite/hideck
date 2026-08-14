package config

import (
	"fmt"
	"os"

	yaml "go.yaml.in/yaml/v3"
)

func ensureServerHTTPSSettingInFile(path string) error {
	configFileMu.Lock()
	defer configFileMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("配置文件结构错误")
	}

	root := document.Content[0]
	server := getMapValue(root, "server")
	if server == nil {
		server = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMapNode(root, "server", server)
	} else if server.Kind != yaml.MappingNode {
		return fmt.Errorf("server 配置必须是对象")
	}
	if getMapValue(server, "https_enabled") != nil {
		return nil
	}

	setMapBool(server, "https_enabled", false)
	out, err := yaml.Marshal(&document)
	if err != nil {
		return fmt.Errorf("序列化配置文件失败: %w", err)
	}
	return writeConfigAtomically(path, out)
}
