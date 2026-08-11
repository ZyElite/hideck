package config

import yaml "go.yaml.in/yaml/v3"

func UpdateSystemInFile(path string, system SystemConfig) error {
	return updateConfigInFile(path, func(root *yaml.Node) error {
		systemNode := getMapValue(root, "system")
		if systemNode == nil || systemNode.Kind != yaml.MappingNode {
			systemNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMapNode(root, "system", systemNode)
		}
		setMapBool(systemNode, "openwrt_dynamic_interfaces", system.OpenWRTDynamicInterfaces)
		return nil
	})
}
