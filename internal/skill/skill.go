package skill

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// ComputeHash returns the computedHash vercel-labs/skills records for dir:
// SHA-256 over the paths and contents of regular files in locale order,
// skipping .git and node_modules.
func ComputeHash(dir string) (string, error) {
	var files []string

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && (d.Name() == ".git" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("compute hash for %q: %w", dir, err)
	}

	slices.SortFunc(files, collate.New(language.Und).CompareString)

	h := sha256.New()
	for _, rel := range files {
		if _, err := io.WriteString(h, rel); err != nil {
			return "", fmt.Errorf("write path %q to hash: %w", rel, err)
		}

		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", fmt.Errorf("read file %q: %w", rel, err)
		}

		if _, err := h.Write(data); err != nil {
			return "", fmt.Errorf("write file %q to hash: %w", rel, err)
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
