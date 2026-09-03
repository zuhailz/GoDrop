package components

import (
	"os"
	"testing"
)

// TestMain mirrors cmd/godrop/main.go so that when copyViaX11Server
// re-invokes this test binary as its detached X11 clipboard helper, the
// child process runs the clipboard server instead of the test suite.
func TestMain(m *testing.M) {
	if code := RunClipboardServerIfRequested(); code >= 0 {
		os.Exit(code)
	}
	os.Exit(m.Run())
}
