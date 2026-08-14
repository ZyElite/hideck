package backend

import (
	"context"
	"fmt"

	"github.com/yibaiba/hideck/pkg/smscodec"
)

func (b *MBIMBackend) smsProvider() (SMSProvider, error) {
	if b.sms == nil {
		return nil, fmt.Errorf("MBIM 原生短信路径已禁用，AT 短信调度器未配置")
	}
	return b.sms, nil
}

func (b *MBIMBackend) SendSMS(ctx context.Context, to, body string) error {
	return b.SendSMSWithOptions(ctx, to, body, smscodec.SubmitOptions{})
}

func (b *MBIMBackend) SendSMSWithOptions(ctx context.Context, to, body string, opts smscodec.SubmitOptions) error {
	provider, err := b.smsProvider()
	if err != nil {
		return err
	}
	if sender, ok := provider.(interface {
		SendSMSWithOptions(context.Context, string, string, smscodec.SubmitOptions) error
	}); ok {
		return sender.SendSMSWithOptions(ctx, to, body, opts)
	}
	if encoding, _ := smscodec.NormalizeSMSEncoding(string(opts.Encoding)); encoding != smscodec.SMSEncodingAuto {
		return fmt.Errorf("AT 短信调度器不支持编码选项: %s", opts.Encoding)
	}
	return provider.SendSMS(ctx, to, body)
}

func (b *MBIMBackend) ReadSMS(ctx context.Context, index int) (*SMS, error) {
	provider, err := b.smsProvider()
	if err != nil {
		return nil, err
	}
	return provider.ReadSMS(ctx, index)
}

func (b *MBIMBackend) DeleteSMS(ctx context.Context, index int) error {
	provider, err := b.smsProvider()
	if err != nil {
		return err
	}
	return provider.DeleteSMS(ctx, index)
}

func (b *MBIMBackend) ListSMS(ctx context.Context) ([]SMSSummary, error) {
	provider, err := b.smsProvider()
	if err != nil {
		return nil, err
	}
	return provider.ListSMS(ctx)
}

func (b *MBIMBackend) DeleteAllSMS(ctx context.Context) error {
	provider, err := b.smsProvider()
	if err != nil {
		return err
	}
	return provider.DeleteAllSMS(ctx)
}

func (b *MBIMBackend) GetSMSC(ctx context.Context) (string, error) {
	provider, err := b.smsProvider()
	if err != nil {
		return "", err
	}
	smsc, ok := provider.(SMSCProvider)
	if !ok {
		return "", fmt.Errorf("AT 短信调度器未实现 SMSCProvider")
	}
	return smsc.GetSMSC(ctx)
}

func (b *MBIMBackend) SetSMSC(ctx context.Context, smsc string) error {
	provider, err := b.smsProvider()
	if err != nil {
		return err
	}
	setter, ok := provider.(interface {
		SetSMSC(context.Context, string) error
	})
	if !ok {
		return fmt.Errorf("AT 短信调度器不支持设置 SMSC")
	}
	return setter.SetSMSC(ctx, smsc)
}
