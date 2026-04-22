package runtime

func init() {
	Default.Register("claude", NewClaude)
	Default.Register("shell", NewShell)
}
