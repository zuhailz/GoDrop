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
		_, rel, ok := strings.Cut(f.Name, "/")
		if !ok || rel == "" {
			continue
		}

		cleanRel := filepath.Clean(rel)
		if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("illegal path in archive: %s", f.Name)
		}

		fpath := filepath.Join(root, cleanRel)

		if f.FileInfo().IsDir() {
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
