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

func TestServicePersistsAsyncCommandAudioAttachmentReturnedBeforeActivation(t *testing.T) {
	service := newTestService(t, map[string]notify.CommandHandler{
		"vocall": func(ctx notify.CommandContext, _ []string) string {
			progress, ok := ctx.(interface{ Progress(string) })
			if !ok {
				t.Fatal("command context does not support progress")
			}
			progress.Progress("发起 VoWiFi 呼叫 / 已接通")
			rich, ok := ctx.(interface {
				ReplyWithAttachments(string, []notify.CommandAttachment)
			})
			if !ok {
				t.Fatal("command context does not support attachments")
			}
			rich.ReplyWithAttachments("发起 VoWiFi 呼叫 / 完成", []notify.CommandAttachment{{
				Type: "audio", Recording: "call_wwan1_20260813_100108.890350649.mp3", ContentType: "audio/mpeg",
			}})
			return "发起 VoWiFi 呼叫 / 已受理"
		},
	})
	if _, err := service.Execute(context.Background(), ExecuteRequest{Input: "/vocall wwan1 888 10"}); err != nil {
		t.Fatal(err)
	}
	events := waitForEvents(t, service, 0, 4)
	if events[1].Text != "发起 VoWiFi 呼叫 / 已受理" || events[2].Text != "发起 VoWiFi 呼叫 / 已接通" {
		t.Fatalf("progress order = %+v", events)
	}
	result := events[3]
	if result.Kind != EventResult || len(result.Attachments) != 1 {
		t.Fatalf("result event = %+v", result)
	}
	if result.Attachments[0].Recording != "call_wwan1_20260813_100108.890350649.mp3" {
		t.Fatalf("attachment = %+v", result.Attachments[0])
	}
	reloaded, err := service.ListEvents(context.Background(), 0, 20)
	if err != nil || len(reloaded) != 4 || len(reloaded[3].Attachments) != 1 {
		t.Fatalf("reloaded events = %+v, err=%v", reloaded, err)
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

func TestServiceListsLatestEventsBeforeCursor(t *testing.T) {
	service := newTestService(t, map[string]notify.CommandHandler{
		"list": func(_ notify.CommandContext, _ []string) string { return "设备列表 / 完成" },
	})
	for range 3 {
		if _, err := service.Execute(context.Background(), ExecuteRequest{Input: "/list"}); err != nil {
			t.Fatal(err)
		}
	}
	all := waitForEvents(t, service, 0, 6)
	latest, err := service.ListEventsBefore(context.Background(), 0, 2)
	if err != nil || latest[0].ID != all[4].ID || latest[1].ID != all[5].ID {
		t.Fatalf("latest events = %+v, %v", latest, err)
	}
	earlier, err := service.ListEventsBefore(context.Background(), latest[0].ID, 2)
	if err != nil || earlier[1].ID != all[3].ID {
		t.Fatalf("earlier events = %+v, %v", earlier, err)
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
