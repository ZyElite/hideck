package imscore

import (
	"fmt"
	"net"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

type secureChannelDial func(string) (net.Conn, error)

func dialSecureChannelWithFallback(dial secureChannelDial) (net.Conn, []string, error) {
	conn, err := dial("tcp")
	if err != nil {
		annotation := "tcp:error=" + strings.TrimSpace(err.Error())
		return nil, []string{annotation}, fmt.Errorf("secure signaling channel TCP dial failed: %w", err)
	}
	return conn, []string{"tcp:ok"}, nil
}

func registerConnNetwork(conn net.Conn) string {
	if conn == nil || conn.RemoteAddr() == nil {
		return ""
	}
	network := strings.ToLower(strings.TrimSpace(conn.RemoteAddr().Network()))
	if strings.HasPrefix(network, "tcp") {
		return "tcp"
	}
	if strings.HasPrefix(network, "udp") {
		return "udp"
	}
	return network
}

func logSecureChannelAttemptResult(annotations []string, err error) {
	if err != nil {
		logging.RunDebug("IMS secure signaling channel failed", "attempts", annotations, "err", err)
		return
	}
	logging.RunDebug("IMS secure signaling channel selected", "attempts", annotations)
}

func logSecureChannelEstablished(conn net.Conn) {
	if conn == nil {
		return
	}
	logging.Info("IMS secure signaling channel established",
		"network", registerConnNetwork(conn), "local", conn.LocalAddr(), "remote", conn.RemoteAddr())
}
