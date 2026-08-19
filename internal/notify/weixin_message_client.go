package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	weixinChannelVersion   = "2.4.3"
	weixinLongPollTimeout  = 25_000
	weixinAppClientVersion = "132099"
	weixinLifecycleTimeout = 10 * time.Second
	weixinMessageUser      = 1
	weixinMessageBot       = 2
	weixinMessageFinished  = 2
	weixinItemText         = 1
)

type weixinMessageClient struct {
	httpClient *http.Client
}

type weixinUpdatesResponse struct {
	Ret                int             `json:"ret"`
	ErrCode            int             `json:"errcode"`
	ErrMsg             string          `json:"errmsg"`
	GetUpdatesBuffer   string          `json:"get_updates_buf"`
	LongPollingTimeout int             `json:"longpolling_timeout_ms"`
	Messages           []weixinMessage `json:"msgs"`
}

type weixinMessage struct {
	FromUserID   string               `json:"from_user_id"`
	ToUserID     string               `json:"to_user_id"`
	RoomID       string               `json:"room_id"`
	ChatRoomID   string               `json:"chat_room_id"`
	MessageID    weixinFlexibleString `json:"message_id"`
	ContextToken string               `json:"context_token"`
	MessageType  int                  `json:"message_type"`
	MsgType      int                  `json:"msg_type"`
	Items        []weixinMessageItem  `json:"item_list"`
}

type weixinMessageItem struct {
	Type     int `json:"type"`
	TextItem struct {
		Text string `json:"text"`
	} `json:"text_item"`
}

type weixinAPIResponse struct {
	Ret     int    `json:"ret"`
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type weixinSendTextRequest struct {
	Credentials  WeixinQRCredentials
	Target       string
	Text         string
	ContextToken string
}

type weixinPostRequest struct {
	Credentials WeixinQRCredentials
	Endpoint    string
	Payload     map[string]any
	Target      any
}

type weixinSendItemRequest struct {
	Credentials  WeixinQRCredentials
	Target       string
	Item         map[string]any
	ContextToken string
}

func newWeixinMessageClient(client *http.Client) *weixinMessageClient {
	return &weixinMessageClient{httpClient: newWeixinILinkClient(client).httpClient}
}

func (c *weixinMessageClient) getUpdates(ctx context.Context, credentials WeixinQRCredentials, syncBuffer string) (weixinUpdatesResponse, error) {
	var response weixinUpdatesResponse
	err := c.post(ctx, weixinPostRequest{
		Credentials: credentials, Endpoint: "/ilink/bot/getupdates",
		Payload: map[string]any{
			"get_updates_buf":        syncBuffer,
			"longpolling_timeout_ms": weixinLongPollTimeout,
		}, Target: &response,
	})
	return response, err
}

func (c *weixinMessageClient) notifyStart(ctx context.Context, credentials WeixinQRCredentials) error {
	return c.notifyLifecycle(ctx, credentials, "notifystart")
}

func (c *weixinMessageClient) notifyStop(ctx context.Context, credentials WeixinQRCredentials) error {
	return c.notifyLifecycle(ctx, credentials, "notifystop")
}

func (c *weixinMessageClient) notifyLifecycle(
	ctx context.Context, credentials WeixinQRCredentials, action string,
) error {
	var response weixinAPIResponse
	endpoint := "/ilink/bot/msg/" + action
	if err := c.post(ctx, weixinPostRequest{
		Credentials: credentials, Endpoint: endpoint,
		Payload: map[string]any{}, Target: &response,
	}); err != nil {
		return err
	}
	if response.Ret != 0 || response.ErrCode != 0 {
		return fmt.Errorf("iLink %s 失败: ret=%d errcode=%d errmsg=%s", action, response.Ret, response.ErrCode, response.ErrMsg)
	}
	return nil
}

func (c *weixinMessageClient) sendText(ctx context.Context, input weixinSendTextRequest) error {
	if strings.TrimSpace(input.Text) == "" {
		return errors.New("微信消息内容不能为空")
	}
	clientID, err := randomHex(16)
	if err != nil {
		return err
	}
	message := map[string]any{
		"from_user_id": "", "to_user_id": input.Target, "client_id": "hideck-weixin-" + clientID,
		"message_type": weixinMessageBot, "message_state": weixinMessageFinished,
		"item_list": []any{map[string]any{"type": weixinItemText, "text_item": map[string]string{"text": input.Text}}},
	}
	if strings.TrimSpace(input.ContextToken) != "" {
		message["context_token"] = input.ContextToken
	}
	var response weixinAPIResponse
	if err := c.post(ctx, weixinPostRequest{
		Credentials: input.Credentials, Endpoint: "/ilink/bot/sendmessage",
		Payload: map[string]any{"msg": message}, Target: &response,
	}); err != nil {
		return err
	}
	if response.Ret != 0 || response.ErrCode != 0 {
		return fmt.Errorf("iLink sendmessage 失败: ret=%d errcode=%d errmsg=%s", response.Ret, response.ErrCode, response.ErrMsg)
	}
	return nil
}

func (c *weixinMessageClient) sendItem(ctx context.Context, input weixinSendItemRequest) error {
	clientID, err := randomHex(16)
	if err != nil {
		return err
	}
	message := map[string]any{
		"from_user_id": "", "to_user_id": input.Target, "client_id": "hideck-weixin-" + clientID,
		"message_type": weixinMessageBot, "message_state": weixinMessageFinished,
		"item_list": []any{input.Item},
	}
	if strings.TrimSpace(input.ContextToken) != "" {
		message["context_token"] = input.ContextToken
	}
	var response weixinAPIResponse
	if err := c.post(ctx, weixinPostRequest{
		Credentials: input.Credentials, Endpoint: "/ilink/bot/sendmessage",
		Payload: map[string]any{"msg": message}, Target: &response,
	}); err != nil {
		return err
	}
	if response.Ret != 0 || response.ErrCode != 0 {
		return fmt.Errorf("iLink sendmessage 失败: ret=%d errcode=%d errmsg=%s", response.Ret, response.ErrCode, response.ErrMsg)
	}
	return nil
}

func (c *weixinMessageClient) post(ctx context.Context, input weixinPostRequest) error {
	input.Payload["base_info"] = map[string]string{"channel_version": weixinChannelVersion}
	body, err := json.Marshal(input.Payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(input.Credentials.BaseURL, "/")+input.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	setWeixinMessageHeaders(request, input.Credentials.Token, len(body))
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("iLink %s 请求失败: %w", input.Endpoint, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("iLink %s 请求失败: HTTP %d", input.Endpoint, response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, weixinQRResponseMax)).Decode(input.Target); err != nil {
		return fmt.Errorf("解析 iLink %s 响应失败: %w", input.Endpoint, err)
	}
	return nil
}

func setWeixinMessageHeaders(request *http.Request, token string, contentLength int) {
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("AuthorizationType", "ilink_bot_token")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Length", strconv.Itoa(contentLength))
	request.Header.Set("X-WECHAT-UIN", randomWeixinUIN())
	request.Header.Set("iLink-App-Id", "bot")
	request.Header.Set("iLink-App-ClientVersion", weixinAppClientVersion)
}

func randomWeixinUIN() string {
	value := make([]byte, 4)
	if _, err := rand.Read(value); err != nil {
		return ""
	}
	decimal := strconv.FormatUint(uint64(binary.BigEndian.Uint32(value)), 10)
	return base64.StdEncoding.EncodeToString([]byte(decimal))
}

// iLink sometimes returns message_id as a string and sometimes as a number.
type weixinFlexibleString string

func (s *weixinFlexibleString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*s = ""
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*s = weixinFlexibleString(value)
		return nil
	}
	*s = weixinFlexibleString(string(data))
	return nil
}

func (s weixinFlexibleString) String() string { return string(s) }

func (m weixinMessage) text() string {
	for _, item := range m.Items {
		if item.Type == weixinItemText && strings.TrimSpace(item.TextItem.Text) != "" {
			return strings.TrimSpace(item.TextItem.Text)
		}
	}
	return ""
}

func (m weixinMessage) chat(accountID string) (kind, id string) {
	roomID := strings.TrimSpace(m.RoomID)
	if roomID == "" {
		roomID = strings.TrimSpace(m.ChatRoomID)
	}
	msgType := m.MsgType
	if msgType == 0 {
		msgType = m.MessageType
	}
	toUserID := strings.TrimSpace(m.ToUserID)
	if roomID != "" || (toUserID != "" && toUserID != accountID && msgType == weixinMessageUser) {
		if roomID == "" {
			roomID = toUserID
		}
		return "group", roomID
	}
	return "direct", strings.TrimSpace(m.FromUserID)
}
