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

// clipboardVerifyCmds maps clipboard copy tools to their read-back counterparts.
// After a successful copy on Linux we run the matching verify command and
// compare the output to the original text so we can report real success.
var clipboardVerifyCmds = map[string][]string{
	"wl-copy": {"wl-paste", "--no-newline"},
	"xclip":   {"xclip", "-selection", "clipboard", "-o"},
	"xsel":    {"xsel", "--clipboard", "--output"},
}

// CopyToClipboard places text on the system clipboard, best-effort, and
// reports whether delivery is confirmed. It tries, in order: the platform's
// clipboard tool (pbcopy, clip, wl-copy, xclip, xsel); an in-process X11
// clipboard owner handed off to a detached helper on linux, which needs no
// external tools; and finally the OSC 52 escape sequence, which terminals
// with clipboard support honor -- the useful path over SSH, where a local
// clipboard tool would write to the wrong machine. Terminals without OSC 52
// support silently ignore the fallback, so false means "could not confirm",
// not "nothing to try".
func CopyToClipboard(text string) bool {
	if copyNative(text) {
		return true
	}
	if copyViaX11Server(text) {
		return true
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	_, _ = fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", encoded)
	return false
}

// copyNative pipes text into the platform's clipboard tool and reports
// whether one was found and accepted the write. On Linux it additionally
// verifies the clipboard contains the expected text, because tools like
// wl-copy can succeed silently when no compositor is running.
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
		if cmd.Run() != nil {
			continue
		}
		// On Linux, verify the clipboard actually contains the text.
		if runtime.GOOS == "linux" || runtime.GOOS == "freebsd" ||
			runtime.GOOS == "openbsd" || runtime.GOOS == "netbsd" {
			if verifyCmd, ok := clipboardVerifyCmds[args[0]]; ok {
				out, err := exec.Command(verifyCmd[0], verifyCmd[1:]...).Output()
				if err != nil || strings.TrimSpace(string(out)) != text {
					continue
				}
			}
		}
		return true
	}
	return false
}

const (
	MaxFeedVisible = 8
)

type FeedKind int

const (
	FeedNeutral FeedKind = iota
	FeedSuccess
	FeedWarning
	FeedError
)

type FeedItem struct {
	Text string
	Kind FeedKind
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
