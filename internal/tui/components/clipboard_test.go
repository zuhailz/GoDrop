package components

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

const clipboardTestString = "godrop-clipboard-test-4F8A2C61-B0D3E79A"

// TestCopyToClipboardDarwin exercises the real macOS clipboard round trip.
// It is skipped elsewhere, where no native clipboard tool is guaranteed.
//
// The system clipboard is process-shared, and the host splash test in another
// package may overwrite it concurrently, so we poll for our value rather than
// assume it stays in place.
func TestCopyToClipboardDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native clipboard round trip only wired up on darwin")
	}
	if _, err := exec.LookPath("pbcopy"); err != nil {
		t.Skip("pbcopy not available")
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		if !CopyToClipboard(clipboardTestString) {
			t.Fatal("CopyToClipboard reported failure on darwin")
		}
		got, err := exec.Command("pbpaste").Output()
		if err != nil {
			t.Fatalf("pbpaste failed: %v", err)
		}
		if string(got) == clipboardTestString {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("clipboard kept changing, never observed %q (last %q)", clipboardTestString, got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestCopyToClipboardWindows exercises the real Windows clipboard round trip
// through clip.exe and reads it back with PowerShell. clip.exe ships with
// every Windows installation, so this runs on every Windows CI job.
//
// The system clipboard is a global, process-shared resource: another test
// package in the same `go test ./...` run (the host splash test) overwrites
// it concurrently. So we keep re-writing and re-reading until our value wins
// a race, rather than assume the clipboard stays put.
func TestCopyToClipboardWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows clipboard round trip only wired up on windows")
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		if !CopyToClipboard(clipboardTestString) {
			t.Fatal("CopyToClipboard reported failure on windows")
		}
		out, err := exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard").Output()
		if err != nil {
			t.Fatalf("Get-Clipboard failed: %v", err)
		}
		if strings.TrimSpace(string(out)) == clipboardTestString {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("clipboard kept changing, never observed %q (last %q)", clipboardTestString, strings.TrimSpace(string(out)))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestCopyToClipboardLinuxX11Server exercises the in-process X11 clipboard
// owner: the copy must confirm ownership without any external clipboard
// tool, and the content must be readable back through the standard X11
// selection protocol. Needs an X display; CI runs the linux suite under
// Xvfb. Read-back uses xclip when it happens to be installed.
func TestCopyToClipboardLinuxX11Server(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("x11 clipboard server only wired up on linux")
	}
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display; CI runs the linux suite under Xvfb")
	}

	want := "godrop-x11-server-clipboard-test"
	if !copyViaX11Server(want) {
		t.Fatal("x11 server copy did not confirm ownership")
	}

	if _, err := exec.LookPath("xclip"); err != nil {
		t.Log("xclip not installed; ownership confirmed, skipping read-back")
		return
	}
	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	if err != nil {
		t.Fatalf("xclip read-back failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}
