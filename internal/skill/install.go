package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/lock"
)

type skillReplacement struct {
	Name   string
	Source fs.FS
	Style  style
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

		replacements[i] = skillReplacement{Name: s.Name, Source: s.FS, Style: vercelStyle}
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
		treeSHA, err := snap.TreeSHA(s.Path)
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

		replacements[i] = skillReplacement{Name: s.Name, Source: source, Style: ghStyle}
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
			Style:  vercelStyle,
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

// ReplaceSkill atomically replaces an installed skill directory with source,
// as gh skill installs it.
func ReplaceSkill(skillName string, source fs.FS, layout lock.Layout) (err error) {
	tx, err := stageSkillReplacements(layout, []skillReplacement{{
		Name:   skillName,
		Source: source,
		Style:  ghStyle,
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
		stagedDir, err := tempPath(layout.Dir, "."+replacement.Name+".staged-*")
		if err == nil {
			err = os.Mkdir(stagedDir, 0o755)
		}

		if err != nil {
			return nil, errors.Join(fmt.Errorf("create staged directory for %q: %w", replacement.Name, err), tx.Cleanup())
		}

		if err := copyDir(replacement.Source, stagedDir, replacement.Style); err != nil {
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
