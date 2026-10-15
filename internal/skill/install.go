package skill

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/lock"
)

type skillReplacement struct {
	Name   string
	Source fs.FS
	// Vercel leaves out the files vercel-labs/skills skips when it copies a
	// skill, for skills recorded in the lockfile.
	Vercel bool
}

type stagedSkillReplacement struct {
	Name      string
	StagedDir string
}

type appliedSkillReplacement struct {
	Name      string
	Dir       string
	BackupDir string
}

type replacementTxn struct {
	layout  lock.Layout
	staged  []stagedSkillReplacement
	applied []appliedSkillReplacement
}

// InstallSkills copies discovered skills to skillsDir and updates the lockfile.
func InstallSkills(skills []DiscoveredSkill, ref SourceRef, layout lock.Layout) (err error) {
	if len(skills) == 0 {
		return errors.New("no skills to install")
	}

	lf, err := layout.ReadLockFile()
	if err != nil {
		return err
	}

	replacements := make([]skillReplacement, len(skills))
	for i, s := range skills {
		hash, err := s.Hash()
		if err != nil {
			return fmt.Errorf("hash %q: %w", s.Name, err)
		}

		replacements[i] = skillReplacement{Name: s.Name, Source: s.FS, Vercel: true}
		lf.Skills[s.Name] = lock.Entry{
			Source:       ref.CanonicalSource,
			Ref:          ref.Ref,
			SourceType:   ref.SourceType,
			SkillPath:    s.SkillMDPath(),
			ComputedHash: hash,
		}
	}

	tx, err := stageSkillReplacements(layout, replacements)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, tx.Cleanup()) }()

	if err := tx.Apply(); err != nil {
		return err
	}

	if err := lock.WriteFile(layout.LockPath(), lf); err != nil {
		return errors.Join(fmt.Errorf("write lockfile: %w", err), tx.Rollback())
	}

	return nil
}

// InstallTrackedSkills copies skills discovered in snap to the destination
// directory, injecting gh-compatible source metadata into each SKILL.md. No
// lockfile is written; the frontmatter metadata is the install record, as
// with gh skill install. Only github sources are supported.
func InstallTrackedSkills(skills []DiscoveredSkill, snap git.Snapshot, ref SourceRef, layout lock.Layout) (err error) {
	if len(skills) == 0 {
		return errors.New("no skills to install")
	}

	if ref.SourceType != lock.SourceGitHub {
		return fmt.Errorf("gh skill format supports only github sources, got %q", ref.SourceType)
	}

	replacements := make([]skillReplacement, len(skills))
	for i, s := range skills {
		treeSHA, err := snap.Tree(s.Path)
		if err != nil {
			return err
		}

		source, err := withMetadata(s.FS, SourceMetadata{
			RepoURL: RepoURLForSource(ref.CanonicalSource),
			Ref:     snap.Ref.Full,
			TreeSHA: treeSHA,
			Path:    s.Path,
			Pinned:  ref.Ref,
		})
		if err != nil {
			return fmt.Errorf("inject metadata for %q: %w", s.Name, err)
		}

		replacements[i] = skillReplacement{Name: s.Name, Source: source}
	}

	tx, err := stageSkillReplacements(layout, replacements)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, tx.Cleanup()) }()

	return tx.Apply()
}

// ApplyCandidateUpdates replaces the skills of candidates with their staged
// content and records the new hashes in lf. On lockfile write failure,
// filesystem changes are rolled back.
func ApplyCandidateUpdates(lf lock.File, candidates []UpdateCandidate, layout lock.Layout) (err error) {
	replacements := make([]skillReplacement, 0, len(candidates))
	for _, c := range candidates {
		replacements = append(replacements, skillReplacement{
			Name:   c.SkillName,
			Source: c.Upstream,
			Vercel: true,
		})

		entry := lf.Skills[c.SkillName]
		entry.ComputedHash = c.LatestHash
		entry.SkillPath = path.Join(c.Path, "SKILL.md")
		lf.Skills[c.SkillName] = entry
	}

	tx, err := stageSkillReplacements(layout, replacements)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, tx.Cleanup()) }()

	if err := tx.Apply(); err != nil {
		return err
	}

	if err := lock.WriteFile(layout.LockPath(), lf); err != nil {
		return errors.Join(fmt.Errorf("write updated lockfile: %w", err), tx.Rollback())
	}

	return nil
}

// ReplaceSkill atomically replaces an installed skill directory with source.
func ReplaceSkill(skillName string, source fs.FS, layout lock.Layout) (err error) {
	tx, err := stageSkillReplacements(layout, []skillReplacement{{
		Name:   skillName,
		Source: source,
	}})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, tx.Cleanup()) }()

	return tx.Apply()
}

func stageSkillReplacements(layout lock.Layout, replacements []skillReplacement) (*replacementTxn, error) {
	if len(replacements) == 0 {
		return nil, errors.New("no skill changes to apply")
	}

	if err := os.MkdirAll(layout.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("create destination directory %q: %w", layout.Dir, err)
	}

	tx := &replacementTxn{layout: layout}
	for _, replacement := range replacements {
		stagedDir, err := os.MkdirTemp(layout.Dir, "."+replacement.Name+".staged-*")
		if err != nil {
			return nil, errors.Join(fmt.Errorf("create staged directory for %q: %w", replacement.Name, err), tx.Cleanup())
		}

		if err := copyDir(replacement.Source, stagedDir, replacement.Vercel); err != nil {
			return nil, errors.Join(fmt.Errorf("stage skill %q: %w", replacement.Name, err), tx.Cleanup())
		}

		tx.staged = append(tx.staged, stagedSkillReplacement{
			Name:      replacement.Name,
			StagedDir: stagedDir,
		})
	}

	return tx, nil
}

func (tx *replacementTxn) Apply() error {
	for _, replacement := range tx.staged {
		backupDir, err := tx.swapIn(replacement)
		if err != nil {
			return errors.Join(err, tx.Rollback())
		}

		tx.applied = append(tx.applied, appliedSkillReplacement{
			Name:      replacement.Name,
			Dir:       tx.layout.SkillDir(replacement.Name),
			BackupDir: backupDir,
		})
	}

	return nil
}

func (tx *replacementTxn) Rollback() error {
	var errs []error

	for _, applied := range slices.Backward(tx.applied) {
		if err := os.RemoveAll(applied.Dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove applied skill %q: %w", applied.Name, err))
			continue
		}

		if applied.BackupDir == "" {
			continue
		}

		if err := os.Rename(applied.BackupDir, applied.Dir); err != nil {
			errs = append(errs, fmt.Errorf("restore backup for %q: %w", applied.Name, err))
		}
	}

	tx.applied = nil

	return errors.Join(errs...)
}

func (tx *replacementTxn) Cleanup() error {
	var errs []error

	for _, replacement := range tx.staged {
		if replacement.StagedDir == "" {
			continue
		}

		if err := os.RemoveAll(replacement.StagedDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("cleanup staged skill %q: %w", replacement.Name, err))
		}
	}

	for _, applied := range tx.applied {
		if applied.BackupDir == "" {
			continue
		}

		if err := os.RemoveAll(applied.BackupDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("cleanup backup for %q: %w", applied.Name, err))
		}
	}

	return errors.Join(errs...)
}

func (tx *replacementTxn) swapIn(replacement stagedSkillReplacement) (string, error) {
	dst := tx.layout.SkillDir(replacement.Name)
	backupDir := ""

	if _, err := os.Stat(dst); err == nil {
		path, err := tempPath(tx.layout.Dir, "."+replacement.Name+".backup-*")
		if err != nil {
			return "", fmt.Errorf("prepare backup for %q: %w", replacement.Name, err)
		}

		if err := os.Rename(dst, path); err != nil {
			return "", fmt.Errorf("backup existing skill %q: %w", replacement.Name, err)
		}

		backupDir = path
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat existing skill %q: %w", replacement.Name, err)
	}

	if err := os.Rename(replacement.StagedDir, dst); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, dst)
		}

		return "", fmt.Errorf("activate staged skill %q: %w", replacement.Name, err)
	}

	return backupDir, nil
}

func tempPath(parentDir, pattern string) (string, error) {
	path, err := os.MkdirTemp(parentDir, pattern)
	if err != nil {
		return "", err
	}

	if err := os.Remove(path); err != nil {
		return "", err
	}

	return path, nil
}

// skipCopy reports whether a copy of a skill leaves d out: .git always, and
// with vercel what vercel-labs/skills skips when it installs a skill.
func skipCopy(d os.DirEntry, vercel bool) bool {
	if d.IsDir() {
		return d.Name() == ".git" || vercel && (d.Name() == "__pycache__" || d.Name() == "__pypackages__")
	}

	return vercel && d.Name() == "metadata.json"
}

// copyDir copies the regular files of src into dst, leaving out what
// skipCopy skips. src must be a directory, not a link to one.
func copyDir(src fs.FS, dst string, vercel bool) error {
	info, err := fs.Stat(src, ".")
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return errors.New("copy: not a directory")
	}

	return fs.WalkDir(src, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return fmt.Errorf("copy %q: not a local path", rel)
		}

		target := filepath.Join(dst, filepath.FromSlash(rel))

		if skipCopy(d, vercel) {
			if d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		if !d.Type().IsRegular() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		return copyFile(src, rel, target, info.Mode().Perm())
	})
}

func copyFile(src fs.FS, rel, target string, perm fs.FileMode) error {
	in, err := src.Open(rel)
	if err != nil {
		return err
	}

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return errors.Join(err, in.Close())
	}

	_, err = io.Copy(out, in)

	return errors.Join(err, out.Close(), in.Close())
}
