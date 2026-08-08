package swu

import (
	"errors"

	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

var ErrCookieRequired = errors.New("需要重新发送带 COOKIE 的 IKE_SA_INIT")

func (s *Session) handleCookie(cookieData []byte) error {
	logger.Debug("收到 COOKIE，重新发送 IKE_SA_INIT", zap.Int("len", len(cookieData)))
	s.cookie = append([]byte(nil), cookieData...)
	s.sendCookie = true
	return nil
}
