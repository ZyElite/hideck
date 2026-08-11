package notify

import (
	"errors"
	"reflect"
	"sort"
	"testing"
)

type commandCapture struct {
	replies []string
}

func (c *commandCapture) Reply(text string) {
	c.replies = append(c.replies, text)
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
	want := []string{"balance", "esim", "help", "list", "rotate", "send", "sms", "status", "switch", "vocall"}
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
	want := []string{"rotate", "switch", "vocall"}
	if !reflect.DeepEqual(dangerous, want) {
		t.Fatalf("dangerous commands = %v, want %v", dangerous, want)
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
