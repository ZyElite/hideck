package notify

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/yibaiba/hideck/internal/config"
)

type registeredCommandChannel struct {
	name     string
	handlers map[string]CommandHandler
	starts   atomic.Int32
}

func (c *registeredCommandChannel) Name() string    { return c.name }
func (*registeredCommandChannel) Send(string) error { return nil }
func (c *registeredCommandChannel) Start() error {
	c.starts.Add(1)
	return nil
}
func (*registeredCommandChannel) Close() error { return nil }
func (c *registeredCommandChannel) RegisterCommand(name string, handler CommandHandler) {
	c.handlers[name] = handler
}

type recordingChannelCommandExecutor struct {
	request ChannelCommandRequest
}

func (e *recordingChannelCommandExecutor) ExecuteChannelCommand(
	ctx CommandContext,
	request ChannelCommandRequest,
	handler CommandHandler,
) string {
	e.request = request
	return handler(ctx, request.Arguments)
}

func TestManagerRoutesRegisteredBotCommandsThroughLateBoundExecutor(t *testing.T) {
	manager := &Manager{commandService: NewCommandService(map[string]CommandHandler{
		"list": func(_ CommandContext, arguments []string) string {
			return "list:" + joinArgs(arguments)
		},
	})}
	channel := &registeredCommandChannel{name: "qq", handlers: make(map[string]CommandHandler)}
	manager.registerCommands([]Channel{channel})

	executor := &recordingChannelCommandExecutor{}
	manager.SetChannelCommandExecutor(executor)
	result := channel.handlers["list"](&commandCapture{}, []string{"wwan1"})

	if result != "list:wwan1" {
		t.Fatalf("result = %q", result)
	}
	if executor.request.Channel != "qq" || executor.request.Name != "list" ||
		len(executor.request.Arguments) != 1 || executor.request.Arguments[0] != "wwan1" {
		t.Fatalf("request = %+v", executor.request)
	}
}

func TestManagerRejectsBotCommandUntilExecutorIsReady(t *testing.T) {
	called := false
	manager := &Manager{commandService: NewCommandService(map[string]CommandHandler{
		"list": func(CommandContext, []string) string {
			called = true
			return "unexpected"
		},
	})}
	channel := &registeredCommandChannel{name: "qq", handlers: make(map[string]CommandHandler)}
	manager.registerCommands([]Channel{channel})

	result := channel.handlers["list"](&commandCapture{}, nil)
	if result != commandCenterUnavailableReply || called {
		t.Fatalf("result = %q, handler called = %v", result, called)
	}
}

func TestManagerDefersCommandReceiversUntilExplicitStart(t *testing.T) {
	channel := &registeredCommandChannel{name: "qq", handlers: make(map[string]CommandHandler)}
	manager, err := NewManagerWithOptions(
		&config.Config{QQ: config.QQConfig{Enabled: true}}, nil,
		ManagerOptions{
			DeferCommandReceiverStart: true,
			QQChannelFactory:          func(config.QQConfig) (Channel, error) { return channel, nil },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	if channel.starts.Load() != 0 {
		t.Fatal("command receiver started before executor wiring")
	}

	manager.SetChannelCommandExecutor(&recordingChannelCommandExecutor{})
	manager.StartCommandReceivers()
	waitUntil(t, time.Second, func() bool { return channel.starts.Load() == 1 })
	manager.StartCommandReceivers()
	if channel.starts.Load() != 1 {
		t.Fatalf("receiver starts = %d, want 1", channel.starts.Load())
	}
}
