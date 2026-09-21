// Package checkpoint stores private, content-addressed repository recovery sets.
package checkpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"vigil/internal/store"
	"vigil/internal/workspace"
)

const (
	SchemaVersion = 1
	maxFileBytes  = 64 << 20
	maxSetBytes   = 512 << 20
	maxEntries    = 100000
)

type RepositorySpec struct {
	ID             string             `json:"id"`
	Root           string             `json:"root"`
	Identity       workspace.Identity `json:"identity"`
	Exclusions     []string           `json:"exclusions"`
	UntrackedScope []string           `json:"untracked_scope"`
}

type IndexEntry struct {
	Path       string `json:"path"`
	Mode       string `json:"mode"`
	OID        string `json:"oid"`
	Stage      int    `json:"stage"`
	BlobDigest string `json:"blob_digest"`
}

type PathEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"` // regular, symlink, deleted
	Mode   uint32 `json:"mode"`
	Digest string `json:"digest,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Source string `json:"source"` // tracked or untracked
}

type RepositoryManifest struct {
	SchemaVersion  int                `json:"schema_version"`
	RepositoryID   string             `json:"repository_id"`
	Identity       workspace.Identity `json:"identity"`
	HeadOID        string             `json:"head_oid"`
	HeadRef        string             `json:"head_ref,omitempty"`
	HeadDigest     string             `json:"head_digest"`
	IndexDigest    string             `json:"index_digest"`
	IndexBlob      string             `json:"index_blob"`
	BundleDigest   string             `json:"bundle_digest"`
	BundleBlob     string             `json:"bundle_blob"`
	Exclusions     []string           `json:"exclusions"`
	UntrackedScope []string           `json:"untracked_scope"`
	IndexEntries   []IndexEntry       `json:"index_entries"`
	Paths          []PathEntry        `json:"paths"`
	Digest         string             `json:"digest"`
}

type SetManifest struct {
	SchemaVersion int                  `json:"schema_version"`
	ID            string               `json:"id"`
	CreatedAt     int64                `json:"created_at"`
	Repositories  []RepositoryManifest `json:"repositories"`
	Digest        string               `json:"digest"`
}

type Store struct {
	Dir   string
	Fault func(point string) error
}

func Open(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for _, child := range []string{"", "blobs", "sets", "incomplete", "tmp"} {
		if err := store.PrivateDir(filepath.Join(abs, child)); err != nil {
			return nil, err
		}
	}
	return &Store{Dir: abs}, nil
}

func (s *Store) checkpoint(point string) error {
	if s.Fault != nil {
		return s.Fault(point)
	}
	return nil
}

func manifestDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := store.Canonical(raw)
	if err != nil {
		return "", err
	}
	return store.Digest(canonical), nil
}

func (m RepositoryManifest) expectedDigest() (string, error) {
	m.Digest = ""
	return manifestDigest(m)
}

func (m SetManifest) expectedDigest() (string, error) {
	m.Digest = ""
	return manifestDigest(m)
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (s *Store) putBlob(content []byte) (string, error) {
	digest := store.Digest(content)
	destination := filepath.Join(s.Dir, "blobs", digest)
	if err := verifyBlob(destination, digest, int64(len(content))); err == nil {
		return digest, nil
	}
	tmp, err := os.CreateTemp(filepath.Join(s.Dir, "tmp"), ".blob-")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(content)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err := os.Link(tmpName, destination); err != nil && !os.IsExist(err) {
		return "", err
	}
	if err := verifyBlob(destination, digest, int64(len(content))); err != nil {
		return "", err
	}
	if err := syncDir(filepath.Dir(destination)); err != nil {
		return "", err
	}
	return digest, nil
}

func verifyBlob(path, digest string, size int64) error {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != size || size < 0 || size > maxSetBytes {
		return errors.New("checkpoint blob is corrupt or not private")
	}
	hashContent, err := io.ReadAll(io.LimitReader(f, maxSetBytes+1))
	if err != nil {
		return err
	}
	if int64(len(hashContent)) != size || store.Digest(hashContent) != digest {
		return errors.New("checkpoint blob digest mismatch")
	}
	return nil
}

func safeGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	base := []string{"-C", root, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "credential.helper=", "-c", "diff.external=", "-c", "core.attributesFile=" + os.DevNull}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(root, ".vigil-no-home"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Git checkpoint observation failed: %s", strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() > maxSetBytes {
		return nil, errors.New("Git checkpoint output exceeds limit")
	}
	return stdout.Bytes(), nil
}

func excluded(path string, exclusions []string) bool {
	path = filepath.ToSlash(path)
	for _, prefix := range exclusions {
		prefix = filepath.ToSlash(filepath.Clean(prefix))
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func allowed(path string, scopes []string) bool {
	for _, scope := range scopes {
		if strings.HasSuffix(scope, "/**") {
			prefix := strings.TrimSuffix(scope, "**")
			if path == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(path, prefix) {
				return true
			}
		}
		if ok, _ := filepath.Match(scope, path); ok {
			return true
		}
	}
	return false
}

func parseIndex(raw []byte) ([]IndexEntry, error) {
	var result []IndexEntry
	for _, record := range bytes.Split(raw, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		parts := bytes.SplitN(record, []byte{'\t'}, 2)
		if len(parts) != 2 {
			return nil, errors.New("invalid Git index entry")
		}
		meta := strings.Fields(string(parts[0]))
		stage, err := strconv.Atoi(metaValue(meta, 2))
		path := filepath.ToSlash(string(parts[1]))
		if err != nil || len(meta) != 3 || stage != 0 || meta[0] == "160000" || path == "" || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, "../") {
			return nil, errors.New("unsupported or invalid Git index entry")
		}
		result = append(result, IndexEntry{Path: path, Mode: meta[0], OID: meta[1], Stage: stage})
	}
	return result, nil
}

func metaValue(values []string, index int) string {
	if index >= len(values) {
		return ""
	}
	return values[index]
}

func readBounded(path string) ([]byte, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return []byte(target), info.Mode(), err
	}
	if !info.Mode().IsRegular() {
		return nil, info.Mode(), errors.New("unsupported checkpoint file kind")
	}
	if info.Size() > maxFileBytes {
		return nil, info.Mode(), errors.New("checkpoint file exceeds 64 MiB")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, info.Mode(), err
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, info.Mode(), err
	}
	if len(content) > maxFileBytes {
		return nil, info.Mode(), errors.New("checkpoint file exceeds 64 MiB")
	}
	now, err := f.Stat()
	if err != nil || !os.SameFile(info, now) || now.Size() != int64(len(content)) {
		return nil, info.Mode(), errors.New("checkpoint source changed during capture")
	}
	return content, info.Mode(), nil
}

func (s *Store) captureRepository(ctx context.Context, spec RepositorySpec) (RepositoryManifest, int64, error) {
	var result RepositoryManifest
	if !store.SafeID(spec.ID) || len(spec.UntrackedScope) == 0 {
		return result, 0, errors.New("repository ID and explicit untracked scope required")
	}
	identity, err := workspace.Inspect(ctx, spec.Root)
	if err != nil || identity.CommonGit == "" {
		return result, 0, errors.New("ordinary Git repository required for checkpoint")
	}
	if spec.Identity.Key != "" && (identity.Key != spec.Identity.Key || identity.CommonGit != spec.Identity.CommonGit) {
		return result, 0, errors.New("repository identity changed before checkpoint")
	}
	if filepath.Join(identity.Root, ".git") != identity.CommonGitPath {
		return result, 0, errors.New("linked worktrees are not supported for checkpoint capture")
	}
	result = RepositoryManifest{SchemaVersion: SchemaVersion, RepositoryID: spec.ID, Identity: identity, Exclusions: append([]string(nil), spec.Exclusions...), UntrackedScope: append([]string(nil), spec.UntrackedScope...)}
	sort.Strings(result.Exclusions)
	sort.Strings(result.UntrackedScope)
	if head, err := safeGit(ctx, identity.Root, "rev-parse", "--verify", "HEAD^{commit}"); err == nil {
		result.HeadOID = strings.TrimSpace(string(head))
	} else {
		return result, 0, err
	}
	if headRef, err := safeGit(ctx, identity.Root, "symbolic-ref", "--quiet", "HEAD"); err == nil {
		result.HeadRef = strings.TrimSpace(string(headRef))
	}
	headBytes, _, err := readBounded(filepath.Join(identity.CommonGitPath, "HEAD"))
	if err != nil {
		return result, 0, err
	}
	result.HeadDigest, err = s.putBlob(headBytes)
	if err != nil {
		return result, 0, err
	}
	indexBytes, _, err := readBounded(filepath.Join(identity.CommonGitPath, "index"))
	if err != nil {
		return result, 0, err
	}
	result.IndexDigest = store.Digest(indexBytes)
	result.IndexBlob, err = s.putBlob(indexBytes)
	if err != nil {
		return result, 0, err
	}
	indexRaw, err := safeGit(ctx, identity.Root, "ls-files", "--stage", "-z")
	if err != nil {
		return result, 0, err
	}
	result.IndexEntries, err = parseIndex(indexRaw)
	if err != nil {
		return result, 0, err
	}
	tracked := map[string]bool{}
	total := int64(len(indexBytes) + len(headBytes))
	filteredIndex := make([]IndexEntry, 0, len(result.IndexEntries))
	for n := range result.IndexEntries {
		entry := result.IndexEntries[n]
		if excluded(entry.Path, result.Exclusions) {
			continue
		}
		tracked[entry.Path] = true
		content, err := safeGit(ctx, identity.Root, "cat-file", "blob", entry.OID)
		if err != nil || len(content) > maxFileBytes {
			return result, total, errors.New("checkpoint cannot read a staged blob")
		}
		entry.BlobDigest, err = s.putBlob(content)
		if err != nil {
			return result, total, err
		}
		total += int64(len(content))
		filteredIndex = append(filteredIndex, entry)
	}
	result.IndexEntries = filteredIndex
	untrackedRaw, err := safeGit(ctx, identity.Root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return result, total, err
	}
	paths := make([]string, 0, len(tracked)+len(untrackedRaw)/16)
	for path := range tracked {
		paths = append(paths, path)
	}
	for _, raw := range bytes.Split(untrackedRaw, []byte{0}) {
		path := filepath.ToSlash(string(raw))
		if path != "" && !excluded(path, result.Exclusions) && allowed(path, spec.UntrackedScope) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) > maxEntries {
		return result, total, errors.New("checkpoint exceeds 100000 paths")
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		source := "untracked"
		if tracked[path] {
			source = "tracked"
		}
		content, mode, err := readBounded(filepath.Join(identity.Root, filepath.FromSlash(path)))
		if os.IsNotExist(err) && source == "tracked" {
			result.Paths = append(result.Paths, PathEntry{Path: path, Kind: "deleted", Source: source})
			continue
		}
		if err != nil {
			return result, total, err
		}
		kind := "regular"
		if mode&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		digest, err := s.putBlob(content)
		if err != nil {
			return result, total, err
		}
		result.Paths = append(result.Paths, PathEntry{Path: path, Kind: kind, Mode: uint32(mode.Perm()), Digest: digest, Size: int64(len(content)), Source: source})
		total += int64(len(content))
		if total > maxSetBytes {
			return result, total, errors.New("checkpoint set exceeds 512 MiB")
		}
	}
	bundle, err := os.CreateTemp(filepath.Join(s.Dir, "tmp"), ".bundle-")
	if err != nil {
		return result, total, err
	}
	bundlePath := bundle.Name()
	bundle.Close()
	defer os.Remove(bundlePath)
	if _, err := safeGit(ctx, identity.Root, "bundle", "create", bundlePath, "HEAD"); err != nil {
		return result, total, err
	}
	bundleBytes, _, err := readBounded(bundlePath)
	if err != nil {
		return result, total, err
	}
	result.BundleDigest, err = s.putBlob(bundleBytes)
	result.BundleBlob = result.BundleDigest
	if err != nil {
		return result, total, err
	}
	total += int64(len(bundleBytes))
	result.Digest, err = result.expectedDigest()
	return result, total, err
}

func (s *Store) Capture(ctx context.Context, id string, createdAt int64, repositories []RepositorySpec) (manifest SetManifest, err error) {
	if !store.SafeID(id) || createdAt <= 0 || len(repositories) == 0 || len(repositories) > 100 {
		return manifest, errors.New("valid checkpoint identity, time and repositories required")
	}
	if existing, readErr := s.Read(ctx, id); readErr == nil {
		return existing, nil
	}
	tmpDir, err := os.MkdirTemp(filepath.Join(s.Dir, "tmp"), ".set-")
	if err != nil {
		return manifest, err
	}
	if err := os.Chmod(tmpDir, 0700); err != nil {
		return manifest, err
	}
	failed := true
	defer func() {
		if !failed {
			return
		}
		failure := map[string]any{"schema_version": SchemaVersion, "id": id, "error": errorString(err), "captured_repositories": len(manifest.Repositories)}
		raw, _ := json.Marshal(failure)
		_ = os.WriteFile(filepath.Join(tmpDir, "failure.json"), raw, 0600)
		_ = syncDir(tmpDir)
		destination := filepath.Join(s.Dir, "incomplete", id)
		if renameErr := os.Rename(tmpDir, destination); renameErr != nil && !os.IsExist(renameErr) && err == nil {
			err = renameErr
		}
		_ = syncDir(filepath.Join(s.Dir, "incomplete"))
	}()
	manifest = SetManifest{SchemaVersion: SchemaVersion, ID: id, CreatedAt: createdAt}
	specs := append([]RepositorySpec(nil), repositories...)
	sort.Slice(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	seen := map[string]bool{}
	var total int64
	for _, spec := range specs {
		if seen[spec.ID] {
			err = errors.New("duplicate repository in checkpoint set")
			return manifest, err
		}
		seen[spec.ID] = true
		repository, bytes, captureErr := s.captureRepository(ctx, spec)
		if captureErr != nil {
			err = captureErr
			return manifest, err
		}
		total += bytes
		if total > maxSetBytes {
			err = errors.New("checkpoint set exceeds 512 MiB")
			return manifest, err
		}
		manifest.Repositories = append(manifest.Repositories, repository)
		if err = s.checkpoint("after_repository:" + spec.ID); err != nil {
			return manifest, err
		}
	}
	manifest.Digest, err = manifest.expectedDigest()
	if err != nil {
		return manifest, err
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return manifest, err
	}
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	f, err := os.OpenFile(manifestPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return manifest, err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return manifest, err
	}
	if err = syncDir(tmpDir); err != nil {
		return manifest, err
	}
	destination := filepath.Join(s.Dir, "sets", id)
	if err = os.Rename(tmpDir, destination); err != nil {
		return manifest, err
	}
	failed = false
	if err = syncDir(filepath.Join(s.Dir, "sets")); err != nil {
		return manifest, err
	}
	if err = s.Verify(ctx, manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func errorString(err error) string {
	if err == nil {
		return "capture interrupted"
	}
	return err.Error()
}

func (s *Store) Read(ctx context.Context, id string) (SetManifest, error) {
	var manifest SetManifest
	if !store.SafeID(id) {
		return manifest, errors.New("invalid checkpoint ID")
	}
	path := filepath.Join(s.Dir, "sets", id, "manifest.json")
	content, _, err := readBounded(path)
	if err != nil {
		return manifest, err
	}
	if err := store.Decode(content, &manifest); err != nil {
		return manifest, err
	}
	if manifest.ID != id {
		return manifest, errors.New("checkpoint manifest identity mismatch")
	}
	return manifest, s.Verify(ctx, manifest)
}

func (s *Store) Verify(ctx context.Context, manifest SetManifest) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if manifest.SchemaVersion != SchemaVersion || !store.SafeID(manifest.ID) || len(manifest.Repositories) == 0 {
		return errors.New("invalid checkpoint set manifest")
	}
	digest, err := manifest.expectedDigest()
	if err != nil || digest != manifest.Digest {
		return errors.New("checkpoint set manifest digest mismatch")
	}
	seen := map[string]bool{}
	for _, repository := range manifest.Repositories {
		if seen[repository.RepositoryID] || repository.SchemaVersion != SchemaVersion {
			return errors.New("invalid checkpoint repository manifest")
		}
		seen[repository.RepositoryID] = true
		digest, err := repository.expectedDigest()
		if err != nil || digest != repository.Digest {
			return errors.New("checkpoint repository manifest digest mismatch")
		}
		refs := map[string]int64{repository.IndexBlob: -1, repository.HeadDigest: -1, repository.BundleBlob: -1}
		for _, entry := range repository.IndexEntries {
			refs[entry.BlobDigest] = -1
		}
		for _, entry := range repository.Paths {
			if entry.Kind != "deleted" {
				refs[entry.Digest] = entry.Size
			}
		}
		for digest, size := range refs {
			if len(digest) != 64 || !store.SafeID(digest) {
				return errors.New("checkpoint has invalid blob reference")
			}
			path := filepath.Join(s.Dir, "blobs", digest)
			if size < 0 {
				info, statErr := os.Lstat(path)
				if statErr != nil {
					return statErr
				}
				size = info.Size()
			}
			if err := verifyBlob(path, digest, size); err != nil {
				return err
			}
		}
		bundlePath := filepath.Join(s.Dir, "blobs", repository.BundleBlob)
		cmd := exec.CommandContext(ctx, "git", "bundle", "list-heads", bundlePath)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(s.Dir, "no-home"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "LC_ALL=C"}
		bundleHeads, err := cmd.Output()
		if err != nil || !bytes.Contains(bundleHeads, []byte(repository.HeadOID)) {
			return errors.New("checkpoint Git bundle is not recovery-readable")
		}
	}
	return nil
}
