package client

import "errors"

var (
	ErrVoiceClientSendTimeout        = errors.New("voice client send timeout")
	ErrVoiceClientTransactionTimeout = errors.New("voice client transaction timeout")
	ErrVoiceClientWriteQueueFull     = errors.New("voice client write queue full")

	errSIPRequestEmpty         = errors.New("client SIP request 为空")
	errWriteRequestEmpty       = errors.New("client write request 为空")
	errTransactionEmpty        = errors.New("client transaction request 为空")
	errClientUninitialized     = errors.New("voice client 未初始化")
	errAdapterUninitialized    = errors.New("voice client adapter 未初始化")
	errWriteQueueUninitialized = errors.New("voice client 写队列未初始化")
)
