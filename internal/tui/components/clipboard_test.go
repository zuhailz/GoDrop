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

	if !CopyToClipboard(clipboardTestString) {
		t.Fatal("CopyToClipboard reported failure on darwin")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := exec.Command("pbpaste").Output()
		if err != nil {
			t.Fatalf("pbpaste failed: %v", err)
		}
		if string(got) == clipboardTestString {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("clipboard = %q, want %q (after retrying for 5s)", got, clipboardTestString)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestCopyToClipboardWindows exercises the real Windows clipboard round trip
// through clip.exe and reads it back with PowerShell. clip.exe ships with
// every Windows installation, so this runs on every Windows CI job.
//
// The system clipboard is a global, process-shared resource: another test
// package in the same `go test ./...` run may overwrite it between our write
// and our read (e.g. the host splash test copies a test room key). We poll
// for the value we wrote with a short deadline rather than assume nothing
// else touches the clipboard.
func TestCopyToClipboardWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows clipboard round trip only wired up on windows")
	}

	if !CopyToClipboard(clipboardTestString) {
		t.Fatal("CopyToClipboard reported failure on windows")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard").Output()
		if err != nil {
			t.Fatalf("Get-Clipboard failed: %v", err)
		}
		if strings.TrimSpace(string(out)) == clipboardTestString {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("clipboard = %q, want %q (after retrying for 5s)", strings.TrimSpace(string(out)), clipboardTestString)
		}
		time.Sleep(100 * time.Millisecond)
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
