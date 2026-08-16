package notify

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type commandCapture struct {
	replies []string
}

func (c *commandCapture) Reply(text string) {
	c.replies = append(c.replies, text)
}

func (c *commandCapture) Confirm(prompt string) bool {
	c.replies = append(c.replies, prompt)
	return true
}

func TestCommandServiceCatalogAndExecution(t *testing.T) {
	service := NewCommandService(map[string]CommandHandler{
		"list": func(_ CommandContext, args []string) string { return "list:" + joinArgs(args) },
	})
	definitions := service.Definitions()
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	want := []string{"balance", "cellcall", "esim", "help", "list", "rotate", "send", "sms", "status", "switch", "vocall"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Definitions() names = %v, want %v", names, want)
	}

	result, err := service.Execute(&commandCapture{}, "/LIST one two")
	if err != nil || result != "list:one,two" {
		t.Fatalf("Execute() = %q, %v", result, err)
	}
}

func TestCommandServiceRejectsUnknownAndInvalidInput(t *testing.T) {
	service := NewCommandService(nil)
	if _, err := service.Execute(&commandCapture{}, "list"); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("Execute(invalid) error = %v", err)
	}
	if _, err := service.Execute(&commandCapture{}, "/missing"); !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("Execute(unknown) error = %v", err)
	}
}

func TestRegisteredHandlerObservesBalanceInjection(t *testing.T) {
	service := NewCommandService(nil)
	handler := service.Handlers()["balance"]
	if err := service.SetHandler("balance", func(_ CommandContext, args []string) string {
		return "balance:" + joinArgs(args)
	}); err != nil {
		t.Fatalf("SetHandler() error = %v", err)
	}
	if got := handler(&commandCapture{}, []string{"wwan0"}); got != "balance:wwan0" {
		t.Fatalf("registered handler = %q", got)
	}
}

func TestDangerousCommandMetadata(t *testing.T) {
	service := NewCommandService(nil)
	var dangerous []string
	for _, definition := range service.Definitions() {
		if definition.Dangerous {
			dangerous = append(dangerous, definition.Name)
		}
	}
	sort.Strings(dangerous)
	want := []string{"cellcall", "rotate", "switch", "vocall"}
	if !reflect.DeepEqual(dangerous, want) {
		t.Fatalf("dangerous commands = %v, want %v", dangerous, want)
	}
}

func TestCommandServiceDefinitionForInput(t *testing.T) {
	service := NewCommandService(nil)
	definition, args, err := service.DefinitionForInput("/send wwan0 10086 hello")
	if err != nil {
		t.Fatalf("DefinitionForInput() error = %v", err)
	}
	if definition.Name != "send" || !definition.Async || len(args) != 3 {
		t.Fatalf("DefinitionForInput() = %+v, %v", definition, args)
	}
}

func TestHelpShowsLiveSortedDeviceIDs(t *testing.T) {
	service := NewCommandService(nil)
	devices := []HelpDevice{{ID: "wwan2"}, {ID: "wwan0", Name: "主卡"}}
	service.SetHelpDevicesProvider(func() []HelpDevice { return devices })

	got, err := service.Execute(&commandCapture{}, "/help")
	if err != nil {
		t.Fatalf("Execute(/help) error = %v", err)
	}
	wantPrefix := "可用设备（2）\n- wwan0  主卡\n- wwan2\n\n命令帮助"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("help = %q, want prefix %q", got, wantPrefix)
	}

	devices = []HelpDevice{{ID: "wwan1", Name: "备用卡"}}
	got, err = service.Execute(&commandCapture{}, "/help")
	if err != nil || !strings.HasPrefix(got, "可用设备（1）\n- wwan1  备用卡") {
		t.Fatalf("live help = %q, err = %v", got, err)
	}
}

func TestHelpShowsEmptyDeviceState(t *testing.T) {
	service := NewCommandService(nil)
	got, err := service.Execute(&commandCapture{}, "/help")
	if err != nil {
		t.Fatalf("Execute(/help) error = %v", err)
	}
	if !strings.HasPrefix(got, "可用设备（0）\n当前没有已配置设备\n\n命令帮助") {
		t.Fatalf("help = %q", got)
	}
}

func joinArgs(args []string) string {
	result := ""
	for index, arg := range args {
		if index > 0 {
			result += ","
		}
		result += arg
	}
	return result
}
