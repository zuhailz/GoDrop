//go:build !linux

package components

// copyViaX11Server is linux-only: every other platform has a guaranteed
// native clipboard tool (pbcopy, clip) handled before this fallback.
func copyViaX11Server(text string) bool { return false }

// runClipboardServer is only ever spawned on linux.
func runClipboardServer(content string) int { return 0 }
