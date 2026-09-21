package workspace

import (
	"bytes"
	"context"
	"crypto/sha1" // Git's default object format; sha256 repositories are handled below.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Baseline is a content-sensitive checkout observation. Exclusions are
// explicit relative path prefixes (nested enrolled repositories plus .git).
// It does not execute hooks, filters, credential helpers or repository tools.
type Baseline struct {
	HeadOID       string   `json:"head_oid"`
	HeadRef       string   `json:"head_ref,omitempty"`
	IndexDigest   string   `json:"index_digest"`
	ContentDigest string   `json:"content_digest"`
	Dirty         bool     `json:"dirty"`
	DirtyPaths    []string `json:"dirty_paths"`
	Exclusions    []string `json:"exclusions"`
}

type EnrollmentObservation struct {
	Identity       Identity `json:"identity"`
	BaseOID        string   `json:"base_oid"`
	RemoteIdentity string   `json:"remote_identity,omitempty"`
	Baseline       Baseline `json:"baseline"`
}

func gitCommand(ctx context.Context, root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	base := []string{"-C", root, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull,
		"-c", "credential.helper=", "-c", "diff.external=", "-c", "core.attributesFile=" + os.DevNull}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Env = gitEnvironment()
	var stdout boundedGitOutput
	var stderr gitOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Git command failed: %s", strings.TrimSpace(stderr.String()))
	}
	return []byte(stdout.String()), nil
}

type boundedGitOutput struct{ bytes.Buffer }

func (b *boundedGitOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 16<<20 {
		return 0, errors.New("Git observation exceeds 16 MiB")
	}
	return b.Buffer.Write(p)
}

func ResolveCommit(ctx context.Context, root, ref string) (string, error) {
	if ref == "" || len(ref) > 512 || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\r\n") {
		return "", errors.New("explicit bounded base ref required")
	}
	b, err := gitCommand(ctx, root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", errors.New("selected base ref does not resolve to a commit")
	}
	oid := strings.TrimSpace(string(b))
	if len(oid) != 40 && len(oid) != 64 {
		return "", errors.New("Git returned an invalid base object identity")
	}
	if _, err := hex.DecodeString(oid); err != nil {
		return "", errors.New("Git returned an invalid base object identity")
	}
	return oid, nil
}

func ValidateBranch(ctx context.Context, root, branch, baseOID string) error {
	if branch == "" || len(branch) > 256 || strings.HasPrefix(branch, "-") || strings.ContainsAny(branch, "\x00\r\n") {
		return errors.New("explicit bounded plan branch required")
	}
	if _, err := gitCommand(ctx, root, "check-ref-format", "--branch", branch); err != nil {
		return errors.New("invalid plan branch")
	}
	b, err := gitCommand(ctx, root, "rev-parse", "--verify", "--end-of-options", "refs/heads/"+branch+"^{commit}")
	if err == nil && strings.TrimSpace(string(b)) != baseOID {
		return errors.New("existing plan branch points to an unrelated object")
	}
	return nil
}

func RemoteIdentity(ctx context.Context, root, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if len(name) > 128 || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\x00\r\n") {
		return "", errors.New("invalid remote name")
	}
	b, err := gitCommand(ctx, root, "remote", "get-url", "--all", name)
	if err != nil {
		return "", errors.New("selected remote is unavailable")
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 1 || lines[0] == "" {
		return "", errors.New("remote must have one unambiguous URL")
	}
	raw := lines[0]
	if u, parseErr := url.Parse(raw); parseErr == nil && u.Scheme != "" {
		if u.User != nil {
			return "", errors.New("credential-bearing remote URLs cannot be persisted")
		}
		u.RawQuery, u.Fragment = "", ""
		raw = u.String()
	} else if at := strings.Index(raw, "@"); at >= 0 && strings.Contains(raw[:at], ":") {
		return "", errors.New("ambiguous credential-bearing remote URL")
	}
	return digestBytes([]byte(raw)), nil
}

func digestBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func normalizedExclusions(root string, exclusions []string) ([]string, error) {
	set := map[string]bool{".git": true}
	for _, item := range exclusions {
		clean := filepath.Clean(filepath.FromSlash(item))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, errors.New("fingerprint exclusion must be a repository-relative path")
		}
		if _, err := os.Lstat(filepath.Join(root, clean)); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		set[filepath.ToSlash(clean)] = true
	}
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	return out, nil
}

func excluded(path string, exclusions []string) bool {
	path = filepath.ToSlash(path)
	for _, prefix := range exclusions {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

type indexEntry struct {
	mode string
	oid  string
}

func readIndex(ctx context.Context, root string) (map[string]indexEntry, []string, error) {
	b, err := gitCommand(ctx, root, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, nil, err
	}
	entries := map[string]indexEntry{}
	var abnormal []string
	for _, record := range bytes.Split(b, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		parts := bytes.SplitN(record, []byte{'\t'}, 2)
		meta := strings.Fields(string(parts[0]))
		if len(parts) != 2 || len(meta) != 3 {
			return nil, nil, errors.New("invalid Git index observation")
		}
		path := filepath.ToSlash(string(parts[1]))
		if meta[2] != "0" || meta[0] == "160000" {
			abnormal = append(abnormal, path)
			continue
		}
		entries[path] = indexEntry{mode: meta[0], oid: meta[1]}
	}
	return entries, abnormal, nil
}

func readTree(ctx context.Context, root, object string) (map[string]indexEntry, error) {
	b, err := gitCommand(ctx, root, "ls-tree", "-r", "-z", "--full-tree", object)
	if err != nil {
		return nil, err
	}
	entries := map[string]indexEntry{}
	for _, record := range bytes.Split(b, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		parts := bytes.SplitN(record, []byte{'\t'}, 2)
		meta := strings.Fields(string(parts[0]))
		if len(parts) != 2 || len(meta) != 3 {
			return nil, errors.New("invalid Git tree observation")
		}
		entries[filepath.ToSlash(string(parts[1]))] = indexEntry{mode: meta[0], oid: meta[2]}
	}
	return entries, nil
}

func blobOID(format string, b []byte) string {
	header := []byte(fmt.Sprintf("blob %d\x00", len(b)))
	if format == "sha256" {
		h := sha256.New()
		h.Write(header)
		h.Write(b)
		return hex.EncodeToString(h.Sum(nil))
	}
	h := sha1.New()
	h.Write(header)
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func Fingerprint(ctx context.Context, root string, exclusions []string) (Baseline, error) {
	var result Baseline
	id, err := Inspect(ctx, root)
	if err != nil || id.CommonGit == "" {
		return result, errors.New("ordinary Git repository required for fingerprinting")
	}
	result.Exclusions, err = normalizedExclusions(id.Root, exclusions)
	if err != nil {
		return result, err
	}
	result.HeadOID, err = ResolveCommit(ctx, id.Root, "HEAD")
	if err != nil {
		return result, err
	}
	if b, refErr := gitCommand(ctx, id.Root, "symbolic-ref", "--quiet", "HEAD"); refErr == nil {
		result.HeadRef = strings.TrimSpace(string(b))
	}
	indexPath := filepath.Join(id.CommonGitPath, "index")
	index, err := os.ReadFile(indexPath)
	if err != nil {
		return result, err
	}
	result.IndexDigest = digestBytes(index)
	entries, abnormal, err := readIndex(ctx, id.Root)
	if err != nil {
		return result, err
	}
	format := "sha1"
	if b, formatErr := gitCommand(ctx, id.Root, "rev-parse", "--show-object-format"); formatErr == nil {
		format = strings.TrimSpace(string(b))
	}
	if format != "sha1" && format != "sha256" {
		return result, errors.New("unsupported Git object format")
	}
	dirty := map[string]bool{}
	headEntries, err := readTree(ctx, id.Root, "HEAD")
	if err != nil {
		return result, err
	}
	for path, indexed := range entries {
		if excluded(path, result.Exclusions) {
			continue
		}
		head, exists := headEntries[path]
		if !exists || head != indexed {
			dirty[path] = true
		}
		delete(headEntries, path)
	}
	for path := range headEntries {
		if !excluded(path, result.Exclusions) {
			dirty[path] = true
		}
	}
	for _, path := range abnormal {
		if !excluded(path, result.Exclusions) {
			dirty[path] = true
		}
	}
	hash := sha256.New()
	count := 0
	err = filepath.WalkDir(id.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(id.Root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if excluded(rel, result.Exclusions) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		count++
		if count > 100000 {
			return errors.New("repository fingerprint exceeds 100000 entries")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		var content []byte
		if info.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(path)
			if readErr != nil {
				return readErr
			}
			content = []byte(target)
		} else if info.Mode().IsRegular() {
			f, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			content, err = io.ReadAll(io.LimitReader(f, (64<<20)+1))
			f.Close()
		} else {
			return errors.New("repository fingerprint rejects special files")
		}
		if err != nil {
			return err
		}
		if len(content) > 64<<20 {
			return errors.New("repository file exceeds 64 MiB fingerprint limit")
		}
		fmt.Fprintf(hash, "%s\x00%o\x00%d\x00", rel, info.Mode().Perm(), len(content))
		hash.Write(content)
		indexed, tracked := entries[rel]
		if !tracked || blobOID(format, content) != indexed.oid {
			dirty[rel] = true
		}
		if tracked {
			modeMatches := indexed.mode == "120000" && info.Mode()&os.ModeSymlink != 0
			if info.Mode().IsRegular() {
				executable := info.Mode().Perm()&0111 != 0
				modeMatches = executable == (indexed.mode == "100755") && (indexed.mode == "100644" || indexed.mode == "100755")
			}
			if !modeMatches {
				dirty[rel] = true
			}
		}
		delete(entries, rel)
		return nil
	})
	if err != nil {
		return result, err
	}
	for path := range entries {
		if !excluded(path, result.Exclusions) {
			dirty[path] = true
		}
	}
	result.ContentDigest = hex.EncodeToString(hash.Sum(nil))
	for path := range dirty {
		result.DirtyPaths = append(result.DirtyPaths, path)
	}
	sort.Strings(result.DirtyPaths)
	result.Dirty = len(result.DirtyPaths) != 0
	return result, id.Validate()
}

func ObserveEnrollment(ctx context.Context, root, baseRef, branch, remote string, exclusions []string) (EnrollmentObservation, error) {
	var result EnrollmentObservation
	var err error
	result.Identity, err = Inspect(ctx, root)
	if err != nil {
		return result, err
	}
	gitPath := filepath.Join(result.Identity.Root, ".git")
	info, statErr := os.Lstat(gitPath)
	if statErr != nil || !info.IsDir() || result.Identity.CommonGitPath != gitPath {
		return result, errors.New("only ordinary repositories are eligible for enrollment")
	}
	result.BaseOID, err = ResolveCommit(ctx, result.Identity.Root, baseRef)
	if err != nil {
		return result, err
	}
	if err = ValidateBranch(ctx, result.Identity.Root, branch, result.BaseOID); err != nil {
		return result, err
	}
	result.RemoteIdentity, err = RemoteIdentity(ctx, result.Identity.Root, remote)
	if err != nil {
		return result, err
	}
	result.Baseline, err = Fingerprint(ctx, result.Identity.Root, exclusions)
	return result, err
}

// PrepareBranch performs only ref/symbolic-HEAD mutations after callers have
// journaled intent. It never checks out files and refuses any baseline drift.
func PrepareBranch(ctx context.Context, identity Identity, branch, baseOID string, expected Baseline) (Baseline, error) {
	var result Baseline
	if err := identity.Validate(); err != nil {
		return result, err
	}
	exclusions := make([]string, 0, len(expected.Exclusions))
	for _, item := range expected.Exclusions {
		if item != ".git" {
			exclusions = append(exclusions, item)
		}
	}
	current, err := Fingerprint(ctx, identity.Root, exclusions)
	if err != nil {
		return result, err
	}
	if current.Dirty || current.HeadOID != expected.HeadOID || current.IndexDigest != expected.IndexDigest || current.ContentDigest != expected.ContentDigest {
		return result, errors.New("repository changed or has unresolved edits; branch preparation postponed")
	}
	if current.HeadOID != baseOID {
		return result, errors.New("selected base moved or checkout is not at the enrolled base")
	}
	if err := ValidateBranch(ctx, identity.Root, branch, baseOID); err != nil {
		return result, err
	}
	ref := "refs/heads/" + branch
	if _, err := gitCommand(ctx, identity.Root, "update-ref", ref, baseOID, strings.Repeat("0", len(baseOID))); err != nil {
		// Existing same-object branch is a safe reconciliation case.
		b, observeErr := gitCommand(ctx, identity.Root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		if observeErr != nil || strings.TrimSpace(string(b)) != baseOID {
			return result, errors.New("plan branch creation failed or collided")
		}
	}
	if _, err := gitCommand(ctx, identity.Root, "symbolic-ref", "HEAD", ref); err != nil {
		return result, err
	}
	result, err = Fingerprint(ctx, identity.Root, exclusions)
	if err != nil {
		return result, err
	}
	if result.HeadRef != ref || result.HeadOID != baseOID || result.Dirty {
		return result, errors.New("prepared branch observation does not match intent")
	}
	return result, nil
}
