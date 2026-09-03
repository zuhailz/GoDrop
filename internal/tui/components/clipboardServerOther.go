//go:build !linux

package components

// copyViaX11Server is linux-only: every other platform has a guaranteed
// native clipboard tool (pbcopy, clip) handled before the X11 fallback.
func copyViaX11Server(text string) bool { return false }

// RunClipboardServerIfRequested is linux-only: the detached X11 clipboard
// helper is never spawned on other platforms, so there is never a server
// to run. The caller treats -1 as "not requested".
func RunClipboardServerIfRequested() int { return -1 }
