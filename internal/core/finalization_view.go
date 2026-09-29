package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"vigil/internal/artifacts"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

// ensurePlanArchiveView materializes the immutable per-plan archive view inside
// the Git-ignored .vigil folder of every accepted repository (R66). The
// durable artifact database remains authoritative; .vigil is a local view, not
// trusted state. The folder is excluded from the accepted fingerprint,
// checkpointing and commits, so writing it never invalidates the exact tree.
// The view is immutable and replay-safe: an interrupted write is repaired by
// repeating the exact archive command.
func (e *Engine) ensurePlanArchiveView(ctx context.Context, record ArchiveRecord) error {
	if e == nil || e.DB == nil || e.DB.Kind != "project" || !store.SafeID(record.PlanID) || record.Revision < 1 ||
		!validDigest(record.ManifestDigest) {
		return errors.New("invalid plan archive view identity")
	}
	verified, manifest, err := e.Archive(ctx, record.PlanID, record.Revision)
	if err != nil || verified != record {
		return errors.New("archive changed before plan view materialization")
	}
	repository, err := artifacts.New(e.DB)
	if err != nil {
		return err
	}
	manifestBytes, err := repository.Read(ctx, record.ManifestID)
	if err != nil || store.Digest(manifestBytes) != record.ManifestDigest {
		return errors.New("factual manifest is missing or corrupt")
	}
	var narrativeBytes []byte
	if record.NarrativeID != "" {
		narrativeBytes, err = repository.Read(ctx, record.NarrativeID)
		if err != nil {
			return err
		}
	}
	for _, accepted := range manifest.Repositories {
		if err := accepted.Identity.Validate(); err != nil {
			return err
		}
		if err := writeRepositoryArchiveView(accepted.Identity, record, manifestBytes, narrativeBytes); err != nil {
			return fmt.Errorf("repository %s archive view: %w", accepted.ID, err)
		}
	}
	return nil
}

// writeRepositoryArchiveView writes .vigil/plans/<plan>/archive/ records into
// one accepted repository root and keeps the folder locally Git-ignored.
func writeRepositoryArchiveView(identity workspace.Identity, record ArchiveRecord, manifestBytes, narrativeBytes []byte) error {
	root, err := os.OpenRoot(identity.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := ensureLocalGitIgnore(identity.CommonGitPath); err != nil {
		return err
	}
	vigil, err := archiveViewChild(root, ".vigil")
	if err != nil {
		return err
	}
	defer vigil.Close()
	plans, err := archiveViewChild(vigil, "plans")
	if err != nil {
		return err
	}
	defer plans.Close()
	plan, err := archiveViewChild(plans, record.PlanID)
	if err != nil {
		return err
	}
	defer plan.Close()
	archive, err := archiveViewChild(plan, "archive")
	if err != nil {
		return err
	}
	defer archive.Close()
	if err := writeImmutableView(archive, fmt.Sprintf("factual-r%d-%s.json", record.Revision, record.ManifestDigest), manifestBytes); err != nil {
		return err
	}
	if narrativeBytes != nil {
		if err := writeImmutableView(archive, fmt.Sprintf("narrative-r%d-%s.json", record.Revision, store.Digest(narrativeBytes)), narrativeBytes); err != nil {
			return err
		}
	}
	return nil
}

func archiveViewChild(parent *os.Root, name string) (*os.Root, error) {
	if name != ".vigil" && !store.SafeID(name) {
		return nil, errors.New("invalid archive view directory name")
	}
	if err := parent.Mkdir(name, 0o700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	info, err := parent.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("archive view path is not a real directory")
	}
	return parent.OpenRoot(name)
}

func writeImmutableView(root *os.Root, name string, content []byte) error {
	if len(content) == 0 || len(content) > 1<<20 {
		return errors.New("archive view record exceeds bound")
	}
	temporary := ".vigil-archive-" + store.ID() + ".tmp"
	f, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	_, writeErr := f.Write(content)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := root.Link(temporary, name); err != nil {
		if !os.IsExist(err) {
			return err
		}
		prior, readErr := root.ReadFile(name)
		if readErr != nil || !bytes.Equal(prior, content) {
			return errors.New("existing archive view differs from the durable record")
		}
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// ensureLocalGitIgnore keeps .vigil out of git status and accidental adds. The
// repository-local info/exclude file is used because it is itself never
// committed or fingerprinted: .gitignore would be accepted user content. The
// append is idempotent and marked; an existing ignore entry is respected.
func ensureLocalGitIgnore(commonGitPath string) error {
	if commonGitPath == "" || !filepath.IsAbs(commonGitPath) {
		return errors.New("repository common Git path required")
	}
	info := filepath.Join(commonGitPath, "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		return err
	}
	exclude := filepath.Join(info, "exclude")
	content, err := os.ReadFile(exclude)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range bytes.Split(content, []byte("\n")) {
		if string(bytes.TrimSpace(line)) == ".vigil/" {
			return nil
		}
	}
	f, err := os.OpenFile(exclude, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	entry := []byte("\n# vigil: local archive view (never committed)\n.vigil/\n")
	if len(content) == 0 || bytes.HasSuffix(content, []byte("\n")) {
		entry = entry[1:]
	}
	_, writeErr := f.Write(entry)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
