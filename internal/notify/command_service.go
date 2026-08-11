package notify

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrInvalidCommand = errors.New("命令必须以 / 开头")
	ErrUnknownCommand = errors.New("未知命令")
)

type CommandDefinition struct {
	Name           string `json:"name"`
	Usage          string `json:"usage"`
	Summary        string `json:"summary"`
	Dangerous      bool   `json:"dangerous"`
	Async          bool   `json:"async"`
	DeviceArgument bool   `json:"device_argument"`
}

type CommandService struct {
	mu          sync.RWMutex
	definitions map[string]CommandDefinition
	handlers    map[string]CommandHandler
}

func NewCommandService(handlers map[string]CommandHandler) *CommandService {
	service := &CommandService{
		definitions: commandDefinitions(),
		handlers:    make(map[string]CommandHandler, len(handlers)+2),
	}
	for name, handler := range handlers {
		service.handlers[name] = handler
	}
	service.handlers["help"] = service.handleHelp
	service.handlers["balance"] = unavailableBalanceHandler
	return service
}

func (s *CommandService) Definitions() []CommandDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	definitions := make([]CommandDefinition, 0, len(s.definitions))
	for _, definition := range s.definitions {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions
}

func (s *CommandService) Execute(ctx CommandContext, input string) (string, error) {
	if ctx == nil {
		return "", errors.New("命令回复上下文不能为空")
	}
	name, args, err := parseCommand(input)
	if err != nil {
		return "", err
	}
	handler := s.handler(name)
	if handler == nil {
		return "", fmt.Errorf("%w: /%s", ErrUnknownCommand, name)
	}
	return handler(ctx, args), nil
}

func (s *CommandService) DefinitionForInput(input string) (CommandDefinition, []string, error) {
	name, args, err := parseCommand(input)
	if err != nil {
		return CommandDefinition{}, nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	definition, ok := s.definitions[name]
	if !ok {
		return CommandDefinition{}, nil, fmt.Errorf("%w: /%s", ErrUnknownCommand, name)
	}
	return definition, args, nil
}

func (s *CommandService) Handlers() map[string]CommandHandler {
	definitions := s.Definitions()
	handlers := make(map[string]CommandHandler, len(definitions))
	for _, definition := range definitions {
		name := definition.Name
		handlers[name] = func(ctx CommandContext, args []string) string {
			handler := s.handler(name)
			if handler == nil {
				return unknownCommandReply("/" + name)
			}
			return handler(ctx, args)
		}
	}
	return handlers
}

func (s *CommandService) SetHandler(name string, handler CommandHandler) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if handler == nil {
		return errors.New("命令处理器不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.definitions[name]; !ok {
		return fmt.Errorf("%w: /%s", ErrUnknownCommand, name)
	}
	s.handlers[name] = handler
	return nil
}

func (s *CommandService) handler(name string) CommandHandler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.handlers[name]
}

func (s *CommandService) handleHelp(_ CommandContext, _ []string) string {
	var builder strings.Builder
	builder.WriteString("命令帮助")
	for _, definition := range s.Definitions() {
		builder.WriteString("\n")
		builder.WriteString(definition.Usage)
		builder.WriteString("  ")
		builder.WriteString(definition.Summary)
	}
	return builder.String()
}

func parseCommand(input string) (string, []string, error) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", nil, ErrInvalidCommand
	}
	name := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	if name == "" || strings.Contains(name, "/") {
		return "", nil, ErrInvalidCommand
	}
	return name, append([]string(nil), fields[1:]...), nil
}

func unavailableBalanceHandler(_ CommandContext, _ []string) string {
	return "余额查询 / 失败\n原因    余额查询服务未配置"
}

func commandDefinitions() map[string]CommandDefinition {
	definitions := []CommandDefinition{
		{Name: "help", Usage: "/help", Summary: "查看命令帮助"},
		{Name: "list", Usage: "/list", Summary: "查看设备列表"},
		{Name: "status", Usage: "/status [设备ID]", Summary: "查看设备状态", DeviceArgument: true},
		{Name: "send", Usage: "/send [设备ID] [手机号] [消息]", Summary: "发送短信", Async: true, DeviceArgument: true},
		{Name: "sms", Usage: "/sms [设备ID]", Summary: "查看最近短信", DeviceArgument: true},
		{Name: "esim", Usage: "/esim [设备ID]", Summary: "查看 eSIM", DeviceArgument: true},
		{Name: "switch", Usage: "/switch [设备ID] [序号或ICCID]", Summary: "切换 eSIM", Dangerous: true, Async: true, DeviceArgument: true},
		{Name: "vocall", Usage: "/vocall [设备ID] [号码] [秒数]", Summary: "发起 VoWiFi 通话", Dangerous: true, Async: true, DeviceArgument: true},
		{Name: "rotate", Usage: "/rotate [设备ID]", Summary: "切换公网 IP", Dangerous: true, Async: true, DeviceArgument: true},
		{Name: "balance", Usage: "/balance [设备ID]", Summary: "查询运营商余额", DeviceArgument: true},
	}
	result := make(map[string]CommandDefinition, len(definitions))
	for _, definition := range definitions {
		result[definition.Name] = definition
	}
	return result
}
