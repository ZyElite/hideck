package notify

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestPrepareWeComMediaFormatMatrix(t *testing.T) {
	directory := t.TempDir()
	mp3Path := writeWeComMediaTestFile(t, directory, "call.mp3", []byte("ID3-mp3"))
	amrPath := writeWeComMediaTestFile(t, directory, "call.amr", []byte("#!AMR\nvoice"))
	amrWBPath := writeWeComMediaTestFile(t, directory, "call-wb.amr", []byte("#!AMR-WB\nvoice"))
	invalidAMRPath := writeWeComMediaTestFile(t, directory, "invalid.amr", []byte("not-amr"))

	tests := []struct {
		name       string
		attachment CommandAttachment
		wantPath   string
		wantType   string
		wantNote   string
	}{
		{
			name: "AMR-NB as native voice even without MP3", attachment: CommandAttachment{
				Path: filepath.Join(directory, "missing.mp3"), SourcePath: amrPath, SourceCodec: "AMR",
			},
			wantPath: amrPath, wantType: "voice",
		},
		{
			name: "AMR-WB as file", attachment: CommandAttachment{
				Path: mp3Path, Codec: "MP3", SourcePath: amrWBPath, SourceCodec: "AMR-WB",
			},
			wantPath: amrWBPath, wantType: "file", wantNote: "AMR-WB",
		},
		{
			name: "MP3 as file", attachment: CommandAttachment{Path: mp3Path, Codec: "MP3"},
			wantPath: mp3Path, wantType: "file", wantNote: "MP3",
		},
		{
			name: "invalid AMR falls back explicitly", attachment: CommandAttachment{
				Path: mp3Path, Codec: "MP3", SourcePath: invalidAMRPath, SourceCodec: "AMR",
			},
			wantPath: mp3Path, wantType: "file", wantNote: "不可用",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := prepareWeComMedia(test.attachment)
			if err != nil {
				t.Fatal(err)
			}
			if plan.path != test.wantPath || plan.mediaType != test.wantType || !strings.Contains(plan.note, test.wantNote) {
				t.Fatalf("plan = %+v", plan)
			}
		})
	}
}

func TestPrepareWeComMediaDowngradesOversizedAMRToFile(t *testing.T) {
	directory := t.TempDir()
	amrPath := writeWeComMediaTestFile(t, directory, "large.amr", []byte("#!AMR\nvoice"))
	if err := os.Truncate(amrPath, weComVoiceMaxSize+1); err != nil {
		t.Fatal(err)
	}
	plan, err := prepareWeComMedia(CommandAttachment{SourcePath: amrPath, SourceCodec: "AMR"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.mediaType != "file" || !strings.Contains(plan.note, "2 MiB") {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestWeComBotCommandAttachmentUploadsExactChunksAndRepliesWithFile(t *testing.T) {
	data := make([]byte, weComUploadChunkSize+3)
	for index := range data {
		data[index] = byte(index % 251)
	}
	var initBody map[string]any
	var uploaded []byte
	var indexes []int
	replies := make(chan map[string]any, 2)
	provider := newWeComWebSocketServer(t, func(conn *websocket.Conn, _ int) {
		authenticateWeComTestConnection(t, conn)
		for {
			frame, err := readWeComTestFrame(conn)
			if err != nil {
				return
			}
			body := frameBody(frame)
			switch frameCommand(frame) {
			case weComCommandUploadInit:
				initBody = body
				respondWeComTestFrame(t, conn, frame, map[string]any{"upload_id": "upload-1"}, 0, "")
			case weComCommandUploadChunk:
				chunk, decodeErr := base64.StdEncoding.DecodeString(body["base64_data"].(string))
				if decodeErr != nil {
					t.Errorf("decode chunk: %v", decodeErr)
					return
				}
				uploaded = append(uploaded, chunk...)
				indexes = append(indexes, int(body["chunk_index"].(float64)))
				ackWeComTestFrame(t, conn, frame)
			case weComCommandUploadFinish:
				respondWeComTestFrame(t, conn, frame, map[string]any{"media_id": "media-1", "type": "file"}, 0, "")
			case weComCommandRespond:
				replies <- frame
				ackWeComTestFrame(t, conn, frame)
			}
		}
	})
	defer provider.Close()
	path := writeWeComMediaTestFile(t, t.TempDir(), "recording.mp3", data)
	channel := newWeComBotTestChannel(
		t, provider, NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json")), nil,
	)
	startDone := startWeComBotTestChannel(channel)
	waitUntil(t, time.Second, func() bool { return channel.currentConnection() != nil })
	commandContext := &weComCommandContext{channel: channel, target: "direct-1", requestID: "callback-1"}
	err := commandContext.respond(context.Background(), weComCommandReply{
		text: "呼叫完成", attachments: []CommandAttachment{{Path: path, Codec: "MP3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	textFrame := receiveWeComTestFrame(t, replies)
	mediaFrame := receiveWeComTestFrame(t, replies)
	closeWeComBotTestChannel(t, channel, startDone)

	digest := fmt.Sprintf("%x", md5.Sum(data))
	if initBody["total_chunks"] != float64(2) || initBody["total_size"] != float64(len(data)) || initBody["md5"] != digest {
		t.Fatalf("init = %#v", initBody)
	}
	if string(uploaded) != string(data) || len(indexes) != 2 || indexes[0] != 0 || indexes[1] != 1 {
		t.Fatalf("uploaded bytes = %d, indexes = %#v", len(uploaded), indexes)
	}
	if frameRequestID(textFrame) != "callback-1" || !strings.Contains(frameMarkdown(textFrame), "MP3") {
		t.Fatalf("text reply = %#v", textFrame)
	}
	if frameRequestID(mediaFrame) != "callback-1" || frameBody(mediaFrame)["msgtype"] != "file" {
		t.Fatalf("media reply = %#v", mediaFrame)
	}
}

func TestWeComBotMediaUploadReportsInitFailureAndStops(t *testing.T) {
	commands := make(chan string, 2)
	provider := newWeComWebSocketServer(t, func(conn *websocket.Conn, _ int) {
		authenticateWeComTestConnection(t, conn)
		for {
			frame, err := readWeComTestFrame(conn)
			if err != nil {
				return
			}
			commands <- frameCommand(frame)
			respondWeComTestFrame(t, conn, frame, nil, 40058, "upload rejected")
		}
	})
	defer provider.Close()
	path := writeWeComMediaTestFile(t, t.TempDir(), "recording.mp3", []byte("ID3-audio"))
	channel := newWeComBotTestChannel(
		t, provider, NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json")), nil,
	)
	startDone := startWeComBotTestChannel(channel)
	waitUntil(t, time.Second, func() bool { return channel.currentConnection() != nil })
	plan, err := prepareWeComMedia(CommandAttachment{Path: path, Codec: "MP3"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = channel.uploadMediaPlan(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "初始化") || !strings.Contains(err.Error(), "40058") {
		t.Fatalf("error = %v", err)
	}
	if command := <-commands; command != weComCommandUploadInit {
		t.Fatalf("command = %q", command)
	}
	select {
	case command := <-commands:
		t.Fatalf("unexpected command after failure: %s", command)
	case <-time.After(50 * time.Millisecond):
	}
	closeWeComBotTestChannel(t, channel, startDone)
}

func TestWeComBotMediaUploadHonorsCancelledContext(t *testing.T) {
	path := writeWeComMediaTestFile(t, t.TempDir(), "recording.mp3", []byte("ID3-audio"))
	plan, err := prepareWeComMedia(CommandAttachment{Path: path, Codec: "MP3"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	channel := &WeComBotChannel{requestTimeout: time.Second, now: time.Now, pending: make(map[string]chan weComPendingResult)}
	_, err = channel.uploadMediaPlan(ctx, plan)
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("error = %v", err)
	}
}

func respondWeComTestFrame(
	t *testing.T,
	conn *websocket.Conn,
	request map[string]any,
	body map[string]any,
	errCode int,
	errMessage string,
) {
	t.Helper()
	writeWeComTestFrame(t, conn, map[string]any{
		"cmd": frameCommand(request), "headers": map[string]string{"req_id": frameRequestID(request)},
		"body": body, "errcode": errCode, "errmsg": errMessage,
	})
}

func writeWeComMediaTestFile(t *testing.T, directory, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
