// Package skill discovers, installs, checks and verifies agent skills.
package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"slices"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// ComputeHash returns the computedHash vercel-labs/skills records for the
// skill in fsys: SHA-256 over the paths and contents of regular files in
// locale order, skipping .git and node_modules.
func ComputeHash(fsys fs.FS) (string, error) {
	var files []string

	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() && p != "." && (d.Name() == ".git" || d.Name() == "node_modules") {
			return fs.SkipDir
		}

		if d.Type().IsRegular() {
			files = append(files, p)
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("compute hash: %w", err)
	}

	slices.SortFunc(files, collate.New(language.Und).CompareString)

	h := sha256.New()
	for _, p := range files {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return "", fmt.Errorf("compute hash: %w", err)
		}

		_, _ = io.WriteString(h, p)
		_, _ = h.Write(data)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
