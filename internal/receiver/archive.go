package receiver

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func extractFolder(src, dest, targetName string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer func() { _ = r.Close() }()

	root := filepath.Join(filepath.Clean(dest), targetName)

	for _, f := range r.File {
		// The zip spec requires forward slashes in entry names, but tolerate
		// senders that wrote native separators (e.g. Windows backslashes):
		// normalize first so parsing and the traversal check below behave
		// identically on every OS.
		name := strings.ReplaceAll(f.Name, "\\", "/")
		// Directory entries end with "/" after normalization; do not rely on
		// FileInfo().IsDir(), which inspects the raw name and misclassifies
		// backslash-terminated entries as files.
		isDir := strings.HasSuffix(name, "/")

		_, rel, ok := strings.Cut(name, "/")
		if !ok || rel == "" {
			continue
		}

		cleanRel := filepath.Clean(rel)
		if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("illegal path in archive: %s", f.Name)
		}

		fpath := filepath.Join(root, cleanRel)

		if isDir {
			if err := os.MkdirAll(fpath, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		w, err := os.Create(fpath)
		if err != nil {
			_ = rc.Close()
			return err
		}

		_, copyErr := io.Copy(w, rc)
		_ = rc.Close()
		closeErr := w.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}

	return nil
}
