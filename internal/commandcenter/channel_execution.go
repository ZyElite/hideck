package commandcenter

import (
	"context"
	"errors"
	"strings"

	"github.com/yibaiba/hideck/internal/notify"
)

func (s *Service) ExecuteChannelCommand(
	reply notify.CommandContext,
	request notify.ChannelCommandRequest,
	handler notify.CommandHandler,
) string {
	if reply == nil {
		return commandFailure(errors.New("命令回复上下文不能为空"))
	}
	if handler == nil {
		return commandFailure(errors.New("命令处理器不能为空"))
	}
	name := strings.ToLower(strings.TrimSpace(request.Name))
	definition, _, err := s.commands.DefinitionForInput("/" + name)
	if err != nil {
		return commandFailure(err)
	}
	execution, event, err := s.createExecution(
		context.Background(), channelCommandInput(name, request.Arguments),
		definition.Name, request.Channel, request.Arguments,
	)
	if err != nil {
		return commandFailure(err)
	}
	s.publish(event)
	commandCtx := &replyContext{
		service: s, executionID: execution.ID, finalReply: definition.Async, downstream: reply,
	}
	result := handler(commandCtx, append([]string(nil), request.Arguments...))
	if definition.Async {
		commandCtx.activate(result)
		return result
	}
	s.finish(execution.ID, resultKind(result), result)
	return result
}

func channelCommandInput(name string, arguments []string) string {
	parts := make([]string, 1, len(arguments)+1)
	parts[0] = "/" + name
	parts = append(parts, arguments...)
	return strings.Join(parts, " ")
}

func normalizeSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return SourceWeb
	}
	return source
}

func commandFailure(err error) string {
	return "命令执行失败\n原因    " + err.Error()
}

func (c *replyContext) forward(content eventContent) {
	if c.downstream == nil {
		return
	}
	if len(content.attachments) == 0 {
		c.downstream.Reply(content.text)
		return
	}
	rich, ok := c.downstream.(interface {
		ReplyWithAttachments(string, []notify.CommandAttachment)
	})
	if ok {
		rich.ReplyWithAttachments(content.text, content.attachments)
		return
	}
	c.downstream.Reply(content.text)
}

func (c *replyContext) forwardProgress(text string) {
	if c.downstream == nil {
		return
	}
	progress, ok := c.downstream.(interface{ Progress(string) })
	if ok {
		progress.Progress(text)
		return
	}
	c.downstream.Reply(text)
}
