package skill

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/h3y6e/skills/internal/git"
	"github.com/h3y6e/skills/internal/lock"
)

type skillReplacement struct {
	Name      string
	SourceDir string
}

type stagedSkillReplacement struct {
	Name      string
	StagedDir string
}

type appliedSkillReplacement struct {
	Name      string
	DestDir   string
	BackupDir string
}

type replacementTxn struct {
	layout  lock.Layout
	staged  []stagedSkillReplacement
	applied []appliedSkillReplacement
}

// InstallSkills copies discovered skills to destDir and updates the lockfile.
func InstallSkills(skills []DiscoveredSkill, ref SourceRef, layout lock.Layout) error {
	if len(skills) == 0 {
		return fmt.Errorf("no skills to install")
	}

	tx, err := stageSkillReplacements(layout, discoveredSkillReplacements(skills))
	if err != nil {
		return err
	}
	defer tx.Cleanup()

	lf, err := readOrInitLockFile(layout.LockPath())
	if err != nil {
		return err
	}

	if err := tx.Apply(); err != nil {
		return err
	}

	for _, s := range skills {
		lf.Skills[s.Name] = lock.Entry{
			Source:       ref.CanonicalSource,
			Ref:          ref.Ref,
			SourceType:   ref.SourceType,
			ComputedHash: s.ComputedHash,
			Dest:         filepath.Clean(layout.DestDir),
		}
	}

	if err := lock.WriteFile(layout.LockPath(), lf); err != nil {
		return rollbackAfterLockFailure(tx, fmt.Errorf("write lockfile: %w", err))
	}

	return nil
}

// InstallTrackedSkills copies discovered skills to the destination directory,
// injecting gh-compatible source metadata into each SKILL.md. No lockfile is
// written; the frontmatter metadata is the install record, as with
// gh skill install. Only github sources are supported.
// repoRoot is the cloned repository the skills were discovered in, and
// resolved is the ref the clone was made from.
func InstallTrackedSkills(ctx context.Context, skills []DiscoveredSkill, repoRoot string, ref SourceRef, resolved git.ResolvedRef, layout lock.Layout) error {
	if len(skills) == 0 {
		return fmt.Errorf("no skills to install")
	}
	if ref.SourceType != "github" {
		return fmt.Errorf("gh skill format supports only github sources, got %q", ref.SourceType)
	}

	tx, err := stageSkillReplacements(layout, discoveredSkillReplacements(skills))
	if err != nil {
		return err
	}
	defer tx.Cleanup()

	for i, s := range skills {
		treeSHA, err := git.TreeSHA(ctx, repoRoot, s.Path)
		if err != nil {
			return fmt.Errorf("resolve tree SHA for %q: %w", s.Name, err)
		}
		meta := SourceMetadata{
			RepoURL: RepoURLForSource(ref.CanonicalSource),
			Ref:     resolved.Full,
			TreeSHA: treeSHA,
			Path:    s.Path,
			Pinned:  ref.Ref,
		}
		if err := injectStagedMetadata(tx.staged[i].StagedDir, meta); err != nil {
			return fmt.Errorf("inject metadata for %q: %w", s.Name, err)
		}
	}

	return tx.Apply()
}

// ApplyCandidateUpdates replaces updated skills and persists hashes to the lockfile.
// On lockfile write failure, filesystem changes are rolled back.
func ApplyCandidateUpdates(lf lock.File, candidates []UpdateCandidate, layout lock.Layout) (lock.File, []string, error) {
	replacements := make([]skillReplacement, 0, len(candidates))
	appliedNames := make([]string, 0, len(candidates))
	updated := lf
	if updated.Skills == nil {
		updated.Skills = map[string]lock.Entry{}
	}

	for _, c := range candidates {
		if c.Status != StatusUpdateAvailable {
			continue
		}

		replacements = append(replacements, skillReplacement{
			Name:      c.SkillName,
			SourceDir: c.StagedDir,
		})

		entry := updated.Skills[c.SkillName]
		entry.ComputedHash = c.LatestHash
		updated.Skills[c.SkillName] = entry
		appliedNames = append(appliedNames, c.SkillName)
	}

	if len(replacements) == 0 {
		return updated, nil, nil
	}

	tx, err := stageSkillReplacements(layout, replacements)
	if err != nil {
		return lf, nil, err
	}
	defer tx.Cleanup()

	if err := tx.Apply(); err != nil {
		return lf, nil, err
	}

	if err := lock.WriteFile(layout.LockPath(), updated); err != nil {
		return lf, nil, rollbackAfterLockFailure(tx, fmt.Errorf("write updated lockfile: %w", err))
	}

	return updated, appliedNames, nil
}

// ReplaceSkill atomically replaces an installed skill directory with staged content.
func ReplaceSkill(skillName, stagedDir string, layout lock.Layout) error {
	tx, err := stageSkillReplacements(layout, []skillReplacement{{
		Name:      skillName,
		SourceDir: stagedDir,
	}})
	if err != nil {
		return err
	}
	defer tx.Cleanup()

	return tx.Apply()
}

func discoveredSkillReplacements(skills []DiscoveredSkill) []skillReplacement {
	replacements := make([]skillReplacement, 0, len(skills))
	for _, s := range skills {
		replacements = append(replacements, skillReplacement{
			Name:      s.Name,
			SourceDir: s.Dir,
		})
	}
	return replacements
}

func stageSkillReplacements(layout lock.Layout, replacements []skillReplacement) (*replacementTxn, error) {
	if len(replacements) == 0 {
		return nil, fmt.Errorf("no skill changes to apply")
	}

	if err := os.MkdirAll(layout.DestDir, 0o755); err != nil {
		return nil, fmt.Errorf("create destination directory %q: %w", layout.DestDir, err)
	}

	tx := &replacementTxn{layout: layout}
	for _, replacement := range replacements {
		stagedDir, err := os.MkdirTemp(layout.DestDir, "."+replacement.Name+".staged-*")
		if err != nil {
			tx.Cleanup()
			return nil, fmt.Errorf("create staged directory for %q: %w", replacement.Name, err)
		}

		if err := copyDir(replacement.SourceDir, stagedDir); err != nil {
			tx.Cleanup()
			return nil, fmt.Errorf("stage skill %q: %w", replacement.Name, err)
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
			rollbackErr := tx.Rollback()
			if rollbackErr != nil {
				return fmt.Errorf("%w (rollback: %v)", err, rollbackErr)
			}
			return err
		}

		tx.applied = append(tx.applied, appliedSkillReplacement{
			Name:      replacement.Name,
			DestDir:   tx.layout.SkillDir(replacement.Name),
			BackupDir: backupDir,
		})
	}

	return nil
}

func (tx *replacementTxn) Rollback() error {
	var errs []error

	for i := len(tx.applied) - 1; i >= 0; i-- {
		applied := tx.applied[i]
		if err := os.RemoveAll(applied.DestDir); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove applied skill %q: %w", applied.Name, err))
			continue
		}

		if applied.BackupDir == "" {
			continue
		}

		if err := os.Rename(applied.BackupDir, applied.DestDir); err != nil {
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
		if err := os.RemoveAll(replacement.StagedDir); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("cleanup staged skill %q: %w", replacement.Name, err))
		}
	}

	for _, applied := range tx.applied {
		if applied.BackupDir == "" {
			continue
		}
		if err := os.RemoveAll(applied.BackupDir); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("cleanup backup for %q: %w", applied.Name, err))
		}
	}

	return errors.Join(errs...)
}

func (tx *replacementTxn) swapIn(replacement stagedSkillReplacement) (string, error) {
	dst := tx.layout.SkillDir(replacement.Name)
	backupDir := ""

	if _, err := os.Stat(dst); err == nil {
		path, err := tempPath(tx.layout.DestDir, "."+replacement.Name+".backup-*")
		if err != nil {
			return "", fmt.Errorf("prepare backup for %q: %w", replacement.Name, err)
		}
		if err := os.Rename(dst, path); err != nil {
			return "", fmt.Errorf("backup existing skill %q: %w", replacement.Name, err)
		}
		backupDir = path
	} else if !os.IsNotExist(err) {
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

func readOrInitLockFile(path string) (lock.File, error) {
	lf, err := lock.ReadFile(path)
	if err == nil {
		return lf, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return lock.File{}, fmt.Errorf("read lockfile: %w", err)
	}

	return lock.File{
		Version: 1,
		Skills:  map[string]lock.Entry{},
	}, nil
}

func rollbackAfterLockFailure(tx *replacementTxn, err error) error {
	rollbackErr := tx.Rollback()
	if rollbackErr == nil {
		return err
	}

	return fmt.Errorf("%w (rollback: %v)", err, rollbackErr)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Close()
}
