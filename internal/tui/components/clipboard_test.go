package components

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

const clipboardTestString = "godrop-clipboard-test-4F8A2C61-B0D3E79A"

// TestCopyToClipboardDarwin exercises the real macOS clipboard round trip.
// It is skipped elsewhere, where no native clipboard tool is guaranteed.
func TestCopyToClipboardDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native clipboard round trip only wired up on darwin")
	}
	if _, err := exec.LookPath("pbcopy"); err != nil {
		t.Skip("pbcopy not available")
	}

	if !CopyToClipboard(clipboardTestString) {
		t.Fatal("CopyToClipboard reported failure on darwin")
	}

	got, err := exec.Command("pbpaste").Output()
	if err != nil {
		t.Fatalf("pbpaste failed: %v", err)
	}
	if string(got) != clipboardTestString {
		t.Fatalf("clipboard = %q, want %q", got, clipboardTestString)
	}
}

// TestCopyToClipboardWindows exercises the real Windows clipboard round trip
// through clip.exe and reads it back with PowerShell. clip.exe ships with
// every Windows installation, so this runs on every Windows CI job.
func TestCopyToClipboardWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows clipboard round trip only wired up on windows")
	}

	if !CopyToClipboard(clipboardTestString) {
		t.Fatal("CopyToClipboard reported failure on windows")
	}

	out, err := exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard").Output()
	if err != nil {
		t.Fatalf("Get-Clipboard failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != clipboardTestString {
		t.Fatalf("clipboard = %q, want %q", got, clipboardTestString)
	}
}

// TestCopyToClipboardLinuxX11 exercises the real Linux clipboard round trip
// through xclip. It needs an X display (CI runs the suite under Xvfb) and
// xclip installed; both are provisioned by the workflow on Linux jobs.
func TestCopyToClipboardLinuxX11(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux clipboard round trip only wired up on linux")
	}
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display; CI runs the linux suite under Xvfb")
	}
	if _, err := exec.LookPath("xclip"); err != nil {
		t.Skip("xclip not installed")
	}

	if !CopyToClipboard(clipboardTestString) {
		t.Fatal("CopyToClipboard reported failure on linux")
	}

	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	if err != nil {
		t.Fatalf("xclip read-back failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != clipboardTestString {
		t.Fatalf("clipboard = %q, want %q", got, clipboardTestString)
	}
}
