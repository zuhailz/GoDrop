package components

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/zuhailz/GoDrop/internal/tui/styles"
)

// CopyToClipboard places text on the system clipboard, best-effort, and
// reports whether delivery is confirmed. It first delegates to the platform's
// clipboard tool (pbcopy, clip, wl-copy, xclip, xsel); if none is available it
// falls back to the OSC 52 escape sequence, which terminals with clipboard
// support honor -- the useful path over SSH, where a local clipboard tool
// would write to the wrong machine. Terminals without OSC 52 support silently
// ignore the fallback, so false means "could not confirm", not "nothing to
// try".
func CopyToClipboard(text string) bool {
	if copyNative(text) {
		return true
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	_, _ = fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", encoded)
	return false
}

// copyNative pipes text into the platform's clipboard tool and reports
// whether one was found and accepted the write.
func copyNative(text string) bool {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "windows":
		candidates = [][]string{{"clip"}}
	case "linux", "freebsd", "openbsd", "netbsd":
		candidates = [][]string{
			{"wl-copy"},                          // Wayland
			{"xclip", "-selection", "clipboard"}, // X11
			{"xsel", "--clipboard", "--input"},   // X11
		}
	}

	for _, args := range candidates {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if cmd.Run() == nil {
			return true
		}
	}
	return false
}

const (
	MaxFeedVisible = 8
	MaxFeedTextLen = 300
)

type FeedKind int

const (
	FeedNeutral FeedKind = iota
	FeedSuccess
	FeedWarning
	FeedError
)

type FeedItem struct {
	Text  string
	Event bool
	Kind  FeedKind
}

func RenderFeedEvent(item FeedItem) string {
	var rendered string
	switch item.Kind {
	case FeedSuccess:
		rendered = styles.SuccessStyle.Render("  ✓ " + item.Text)
	case FeedWarning:
		rendered = styles.WarningStyle.Render("  ⚠ " + item.Text)
	case FeedError:
		rendered = styles.ErrorStyle.Render("  ✗ " + item.Text)
	default:
		rendered = styles.NormalItemStyle.Render("  • " + item.Text)
	}
	return rendered + "\n"
}

func SanitizeText(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	s = b.String()

	runes := []rune(s)
	if len(runes) > maxLen {
		s = string(runes[:maxLen]) + "…"
	}
	return s
}
