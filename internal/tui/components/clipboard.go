package components

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/zuhailz/GoDrop/internal/tui/styles"
)

func CopyToClipboard(text string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	// Best-effort: terminals without OSC52 support silently ignore this.
	_, _ = fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", encoded)
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
