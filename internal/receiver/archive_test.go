package receiver

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// zipEntry is one archive entry; a slice keeps entry order deterministic.
type zipEntry struct {
	name    string
	content string
}

// writeTestZip builds a zip file with the given entries, in order.
func writeTestZip(t *testing.T, entries []zipEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}

	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("create entry %q: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.content)); err != nil {
			t.Fatalf("write entry %q: %v", e.name, err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
	return path
}

// TestExtractFolderMixedSeparators verifies entries are extracted whether the
// archive was written with forward slashes (spec) or backslashes (a Windows
// sender predating the fix). This must hold on every OS, not just Windows.
func TestExtractFolderMixedSeparators(t *testing.T) {
	src := writeTestZip(t, []zipEntry{
		{"myfolder/", ""},
		{"myfolder/a.txt", "alpha content here"},
		{`myfolder\sub\`, ""},
		{"myfolder/sub/b.txt", "beta content"},
	})

	dest := t.TempDir()
	if err := extractFolder(src, dest, "myfolder"); err != nil {
		t.Fatalf("extractFolder failed: %v", err)
	}

	gotA, err := os.ReadFile(filepath.Join(dest, "myfolder", "a.txt"))
	if err != nil || !bytes.Equal(gotA, []byte("alpha content here")) {
		t.Fatalf("a.txt mismatch: %v (got %q)", err, gotA)
	}

	gotB, err := os.ReadFile(filepath.Join(dest, "myfolder", "sub", "b.txt"))
	if err != nil || !bytes.Equal(gotB, []byte("beta content")) {
		t.Fatalf("sub/b.txt mismatch: %v (got %q)", err, gotB)
	}
}

// TestExtractFolderRejectsTraversal verifies zip-slip attempts are rejected on
// every OS, including backslash traversals that only look dangerous on Windows.
func TestExtractFolderRejectsTraversal(t *testing.T) {
	for _, entry := range []string{
		"myfolder/../../evil.txt",
		`myfolder\..\..\evil.txt`,
	} {
		src := writeTestZip(t, []zipEntry{{entry, "evil"}})
		dest := t.TempDir()

		if err := extractFolder(src, dest, "myfolder"); err == nil {
			t.Errorf("extractFolder(%q) succeeded, want traversal rejection", entry)
		}

		if _, err := os.Stat(filepath.Join(dest, "evil.txt")); err == nil {
			t.Errorf("entry %q escaped the destination directory", entry)
		}
	}
}
