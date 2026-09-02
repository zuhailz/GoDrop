package host

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zuhailz/GoDrop/internal/host"
)

// TestSplashDismissStillCopies verifies the documented behavior that "c"
// copies on the very first keypress, even while the splash is up, and that
// any other first keypress merely dismisses the splash.
func TestSplashDismissStillCopies(t *testing.T) {
	h, err := host.NewHost(nil, "AAAA1111-BBBB2222-CCCC3333-DDDD4444", "testhost")
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
	found := false
	for _, item := range m.feed {
		if strings.Contains(item.Text, "Copied room key") {
			found = true
		}
	}
	if !found {
		t.Error("c during splash did not record a copy in the feed")
	}

	// A non-c first keypress must only dismiss the splash.
	h2, _ := host.NewHost(nil, "EEEE5555-FFFF6666-AAAA7777-BBBB8888", "testhost2")
	m2 := NewModel(h2)
	xKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	updated2, _ := m2.Update(xKey)
	m2 = updated2.(model)
	if m2.splash {
		t.Error("splash should be dismissed by any keypress")
	}
	for _, item := range m2.feed {
		if strings.Contains(item.Text, "Copied room key") {
			t.Error("x keypress must not record a copy")
		}
	}
}
