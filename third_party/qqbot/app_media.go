package qqbot

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/iniwex5/qqbot/internal/rest"
)

func (a *App) sendVoice(ctx context.Context, delivery Delivery) (Receipt, error) {
	recipientKind, err := mediaRecipientKind(delivery.To)
	if err != nil {
		return Receipt{}, err
	}
	mediaPath := strings.TrimSpace(delivery.MediaPath)
	if mediaPath == "" {
		return Receipt{}, errors.New("voice media_path 不能为空")
	}
	fileName := strings.TrimSpace(delivery.FileName)
	if fileName == "" {
		fileName = filepath.Base(mediaPath)
	}
	request := rest.MediaRequest{
		RecipientKind: recipientKind,
		RecipientID:   strings.TrimSpace(delivery.To.ID),
		FileType:      rest.MediaTypeVoice,
		Path:          mediaPath,
		FileName:      fileName,
		Content:       strings.TrimSpace(delivery.Body),
	}
	if delivery.Reply != nil {
		request.Reply = &rest.ReplyRequest{
			MessageID: strings.TrimSpace(delivery.Reply.MessageID),
			Sequence:  delivery.Reply.Sequence,
			EventID:   strings.TrimSpace(delivery.Reply.EventID),
		}
	}
	result, err := a.delivery.SendMedia(ctx, request)
	if err != nil {
		return Receipt{}, err
	}
	return Receipt{ID: result.ID, At: result.At}, nil
}

func mediaRecipientKind(recipient Recipient) (string, error) {
	if strings.TrimSpace(recipient.ID) == "" {
		return "", errors.New("recipient.id 不能为空")
	}
	switch recipient.Kind {
	case DirectRecipient:
		return "c2c", nil
	case GroupRecipient:
		return "group", nil
	case ChannelRecipient:
		return "", errors.New("QQ 频道不支持此富媒体上传路径")
	default:
		return "", errors.New("recipient.kind 只支持 direct/group/channel")
	}
}
