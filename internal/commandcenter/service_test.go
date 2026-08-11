package commandcenter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/internal/notify"
	"gorm.io/gorm"
)

type commandReplyCapture struct{}

func (commandReplyCapture) Reply(string) {}

func TestServicePersistsAsyncCommandTimeline(t *testing.T) {
	service := newTestService(t, map[string]notify.CommandHandler{
		"send": func(ctx notify.CommandContext, _ []string) string {
			ctx.Reply("发送短信 / 完成")
			return "发送短信 / 已受理"
		},
	})
	execution, err := service.Execute(context.Background(), ExecuteRequest{Input: "/send wwan0 10086 test"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	events := waitForEvents(t, service, 0, 3)
	if events[0].Kind != EventAccepted || events[1].Kind != EventProgress || events[2].Kind != EventResult {
		t.Fatalf("event kinds = %q, %q, %q", events[0].Kind, events[1].Kind, events[2].Kind)
	}
	if events[2].Execution == nil || events[2].Execution.ID != execution.ID || events[2].Execution.State != StateCompleted {
		t.Fatalf("completed execution = %+v", events[2].Execution)
	}
}

func TestServiceCursorAndClearPreserveRunning(t *testing.T) {
	service := newTestService(t, map[string]notify.CommandHandler{
		"list": func(_ notify.CommandContext, _ []string) string { return "设备列表 / 完成" },
		"send": func(_ notify.CommandContext, _ []string) string { return "发送短信 / 已受理" },
	})
	if _, err := service.Execute(context.Background(), ExecuteRequest{Input: "/list"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(context.Background(), ExecuteRequest{Input: "/send wwan0 10086 test"}); err != nil {
		t.Fatal(err)
	}
	events := waitForEvents(t, service, 0, 4)
	after := events[1].ID
	cursorEvents, err := service.ListEvents(context.Background(), after, 20)
	if err != nil || len(cursorEvents) != len(events)-2 {
		t.Fatalf("ListEvents(after) = %d, %v", len(cursorEvents), err)
	}
	deleted, err := service.ClearCompleted(context.Background())
	if err != nil || deleted != 1 {
		t.Fatalf("ClearCompleted() = %d, %v", deleted, err)
	}
	remaining, err := service.ListEvents(context.Background(), 0, 20)
	if err != nil || len(remaining) != 2 || remaining[0].Execution.State != StateRunning {
		t.Fatalf("remaining events = %+v, %v", remaining, err)
	}
}

func newTestService(t *testing.T, handlers map[string]notify.CommandHandler) *Service {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CommandExecution{}, &db.CommandEvent{}); err != nil {
		t.Fatal(err)
	}
	return NewService(notify.NewCommandService(handlers), NewDatabaseStore(database))
}

func waitForEvents(t *testing.T, service *Service, after uint64, count int) []Event {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events, err := service.ListEvents(context.Background(), after, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) >= count {
			return events
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d events", count)
	return nil
}
