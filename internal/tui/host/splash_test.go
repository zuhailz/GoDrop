package host

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zuhailz/GoDrop/internal/host"
)

const testRoomKey = "AAAA1111-BBBB2222-CCCC3333-DDDD4444"

// TestSplashDismissStillCopies verifies that "c" reaches the copy path on the
// very first keypress, while the splash is still up. Whether the copy itself
// is then confirmed depends on the platform: darwin has pbcopy, while a CI
// Linux box may have no clipboard tool at all -- there the feed honestly
// reports that the copy could not be confirmed. Either outcome proves the
// keypress reached the copy path; no outcome at all is the regression this
// test guards against.
func TestSplashDismissStillCopies(t *testing.T) {
	h, err := host.NewHost(nil, testRoomKey, "testhost")
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	m := NewModel(h)

	if !m.splash {
		t.Fatal("model should start in splash state")
	}

	cKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}
	updated, _ := m.Update(cKey)
	m = updated.(model)

	if m.splash {
		t.Error("splash should be dismissed by the c keypress")
	}

	var copied, reported bool
	for _, item := range m.feed {
		if strings.Contains(item.Text, "Copied room key") {
			copied = true
		}
		if strings.Contains(item.Text, "Could not confirm copy") {
			reported = true
		}
	}
	if copied == reported {
		t.Errorf("c during splash must produce exactly one copy outcome (copied=%v, reported=%v)", copied, reported)
	}

	// A non-c first keypress must only dismiss the splash, never copy.
	h2, err := host.NewHost(nil, "EEEE5555-FFFF6666-AAAA7777-BBBB8888", "testhost2")
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	m2 := NewModel(h2)
	xKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	updated2, _ := m2.Update(xKey)
	m2 = updated2.(model)
	if m2.splash {
		t.Error("splash should be dismissed by any keypress")
	}
	for _, item := range m2.feed {
		if strings.Contains(item.Text, "Copied room key") || strings.Contains(item.Text, "Could not confirm copy") {
			t.Error("x keypress must not reach the copy path")
		}
	}
}

