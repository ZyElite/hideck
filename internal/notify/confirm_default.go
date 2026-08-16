package notify

// defaultConfirm is the default Confirm implementation for command contexts
// that don't implement interactive confirmation. It sends the prompt and
// returns true (skip confirmation, proceed). Channels that support interactive
// replies can override this method to wait for /y or /n.
func defaultConfirm(ctx CommandContext, prompt string) bool {
	ctx.Reply(prompt)
	return true
}
