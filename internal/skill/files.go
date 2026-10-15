package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// style is how an install lays out the files of a skill.
type style int

const (
	// installed reads an installed skill: its regular files as they are.
	installed style = iota
	// ghStyle installs as gh skill does: every file 0644, and a link as a
	// file holding its target.
	ghStyle
	// vercelStyle installs as vercel-labs/skills does: modes kept, a link
	// to a file in the repository as that file, and its excluded files left
	// out. Links leading out of the repository are left out too.
	vercelStyle
)

// skillFile is a file an install of a skill holds.
type skillFile struct {
	path string
	mode fs.FileMode
	data []byte
}

// walkSkill calls dir for each directory and file for each file an install
// of the skill in src holds in style s. src must be a directory, not a link
// to one.
func walkSkill(src fs.FS, s style, dir func(path string) error, file func(skillFile) error) error {
	info, err := fs.Stat(src, ".")
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return errors.New("skill: not a directory")
	}

	return walkTree(src, ".", s, []fs.FileInfo{info}, dir, file)
}

// walkTree walks root in src. followed holds the directories entered so far
// through links, so that a link back into one of them ends the walk.
func walkTree(src fs.FS, root string, s style, followed []fs.FileInfo, dir func(string) error, file func(skillFile) error) error {
	return fs.WalkDir(src, root, func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return fmt.Errorf("%q: not a local path", rel)
		}

		if excluded(d, s) {
			if d.IsDir() {
				return fs.SkipDir
			}

			return nil
		}

		if d.IsDir() {
			return dir(rel)
		}

		// vercel-labs/skills copies a link to a directory as the directory.
		if s == vercelStyle && d.Type()&fs.ModeSymlink != 0 {
			if info, err := fs.Stat(src, rel); err == nil && info.IsDir() {
				if slices.ContainsFunc(followed, func(f fs.FileInfo) bool { return os.SameFile(f, info) }) {
					return nil
				}

				return walkTree(src, rel, s, append(slices.Clip(followed), info), dir, file)
			}
		}

		f, ok, err := readSkillFile(src, rel, d, s)
		if err != nil || !ok {
			return err
		}

		return file(f)
	})
}

// excluded reports whether an install in style s leaves d out: .git always,
// and what vercel-labs/skills skips.
func excluded(d fs.DirEntry, s style) bool {
	if d.IsDir() {
		return d.Name() == ".git" || s == vercelStyle && (d.Name() == "__pycache__" || d.Name() == "__pypackages__")
	}

	return s == vercelStyle && d.Name() == "metadata.json"
}

func readSkillFile(src fs.FS, rel string, d fs.DirEntry, s style) (skillFile, bool, error) {
	switch {
	case d.Type().IsRegular():
		info, err := d.Info()
		if err != nil {
			return skillFile{}, false, err
		}

		data, err := fs.ReadFile(src, rel)
		mode := info.Mode().Perm()

		if s == ghStyle {
			mode = 0o644
		}

		return skillFile{rel, mode, data}, true, err
	case d.Type()&fs.ModeSymlink == 0 || s == installed:
		return skillFile{}, false, nil
	case s == ghStyle:
		target, err := fs.ReadLink(src, rel)
		return skillFile{rel, 0o644, []byte(target)}, true, err
	}

	// Reading follows the link, which fails when it leads out of src's root.
	if info, err := fs.Stat(src, rel); err == nil && info.Mode().IsRegular() {
		data, err := fs.ReadFile(src, rel)
		return skillFile{rel, info.Mode().Perm(), data}, true, err
	}

	return skillFile{}, false, nil
}

// copyDir writes the files an install of the skill in src holds in style s
// into dst.
func copyDir(src fs.FS, dst string, s style) error {
	return walkSkill(src, s,
		func(rel string) error { return os.MkdirAll(filepath.Join(dst, filepath.FromSlash(rel)), 0o755) },
		func(f skillFile) error {
			return os.WriteFile(filepath.Join(dst, filepath.FromSlash(f.path)), f.data, f.mode)
		})
}

// readFiles returns the content of each file an install of the skill in
// fsys holds in style s.
func readFiles(fsys fs.FS, s style) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := walkSkill(fsys, s,
		func(string) error { return nil },
		func(f skillFile) error {
			files[f.path] = f.data
			return nil
		})

	return files, err
}
