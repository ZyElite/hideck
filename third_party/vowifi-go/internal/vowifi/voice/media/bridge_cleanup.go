package media

func (b *Bridge) replaceRelay(relay *RTPRelay) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.relay == relay {
		return nil
	}
	if b.relay != nil {
		if err := b.relay.StopCurrent(); err != nil {
			return err
		}
	}
	b.relay = relay
	return nil
}
