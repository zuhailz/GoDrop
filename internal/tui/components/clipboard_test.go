package components

import (
	"os/exec"
	"runtime"
	"testing"
)

// TestCopyToClipboardDarwin exercises the real macOS clipboard round trip.
// It is skipped elsewhere, where no native clipboard tool is guaranteed.
func TestCopyToClipboardDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native clipboard round trip only wired up on darwin")
	}
	if _, err := exec.LookPath("pbcopy"); err != nil {
		t.Skip("pbcopy not available")
	}

	want := "godrop-clipboard-test-4F8A2C61-B0D3E79A"
	if !CopyToClipboard(want) {
		t.Fatal("CopyToClipboard reported failure on darwin")
	}

	got, err := exec.Command("pbpaste").Output()
	if err != nil {
		t.Fatalf("pbpaste failed: %v", err)
	}
	if string(got) != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}
