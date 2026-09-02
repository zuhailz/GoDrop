package host

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func createArchive(dirPath string) (string, error) {
	f, err := os.CreateTemp("", "godrop-folder-*.zip")
	if err != nil {
		return "", fmt.Errorf("failed to create archive: %w", err)
	}

	zw := zip.NewWriter(f)
	root := filepath.Dir(dirPath)

	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		if info.IsDir() {
			_, err = zw.Create(rel + "/")
			return err
		}

		w, err := zw.Create(rel)
		if err != nil {
			return err
		}

		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }()

		_, err = io.Copy(w, src)
		return err
	})

	if err != nil {
		_ = zw.Close()
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("failed to archive folder: %w", err)
	}

	if err := zw.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("failed to finalize archive: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("failed to close archive: %w", err)
	}

	return f.Name(), nil
}
