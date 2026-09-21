package checkpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vigil/internal/store"
	"vigil/internal/workspace"
)

func git(t *testing.T, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(args, err, string(out))
	}
	return out
}

func gitInput(t *testing.T, input string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(args, err, string(out))
	}
	return out
}

func write(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
}

func repositoryFixture(t *testing.T, root string) workspace.Identity {
	t.Helper()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "same.txt"), []byte("base\n"), 0600)
	write(t, filepath.Join(root, "delete.txt"), []byte("delete me\n"), 0600)
	write(t, filepath.Join(root, "binary.dat"), []byte{0, 1, 2, 0xff}, 0600)
	write(t, filepath.Join(root, "script.sh"), []byte("#!/bin/sh\nexit 0\n"), 0600)
	if err := os.Symlink("same.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	git(t, "init", "-q", "-b", "main", root)
	git(t, "-C", root, "add", ".")
	git(t, "-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "base")
	write(t, filepath.Join(root, "same.txt"), []byte("staged\n"), 0600)
	git(t, "-C", root, "add", "same.txt")
	write(t, filepath.Join(root, "same.txt"), []byte("unstaged\n"), 0600)
	write(t, filepath.Join(root, "untracked.txt"), []byte("untracked\n"), 0600)
	write(t, filepath.Join(root, "binary.dat"), []byte{9, 0, 8, 0xff}, 0600)
	if err := os.Remove(filepath.Join(root, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "script.sh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("untracked.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	identity, err := workspace.Inspect(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func findRepository(t *testing.T, manifest SetManifest, id string) RepositoryManifest {
	t.Helper()
	for _, repository := range manifest.Repositories {
		if repository.RepositoryID == id {
			return repository
		}
	}
	t.Fatal("repository missing", id)
	return RepositoryManifest{}
}

func findPath(t *testing.T, manifest RepositoryManifest, path string) PathEntry {
	t.Helper()
	for _, entry := range manifest.Paths {
		if entry.Path == path {
			return entry
		}
	}
	t.Fatal("path missing", path)
	return PathEntry{}
}

func TestCaptureVerifiesMixedRepositoryState(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repository")
	identity := repositoryFixture(t, root)
	checkpointStore, err := Open(filepath.Join(base, "private", "checkpoints"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := checkpointStore.Capture(context.Background(), "set-1", 1, []RepositorySpec{{ID: "repo", Root: root, Identity: identity, Exclusions: []string{".git"}, UntrackedScope: []string{"**"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkpointStore.Verify(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	read, err := checkpointStore.Read(context.Background(), "set-1")
	if err != nil || read.Digest != manifest.Digest {
		t.Fatal(read, err)
	}
	repository := findRepository(t, manifest, "repo")
	indexBytes, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil || store.Digest(indexBytes) != repository.IndexDigest {
		t.Fatal("index bytes not preserved", err)
	}
	storedIndex, err := os.ReadFile(filepath.Join(checkpointStore.Dir, "blobs", repository.IndexBlob))
	if err != nil || !bytes.Equal(storedIndex, indexBytes) {
		t.Fatal("checkpoint index differs", err)
	}
	if entry := findPath(t, repository, "same.txt"); entry.Source != "tracked" || entry.Digest != store.Digest([]byte("unstaged\n")) {
		t.Fatal("mixed staged/unstaged path lost", entry)
	}
	if entry := findPath(t, repository, "untracked.txt"); entry.Source != "untracked" || entry.Digest != store.Digest([]byte("untracked\n")) {
		t.Fatal("untracked path lost", entry)
	}
	if entry := findPath(t, repository, "delete.txt"); entry.Kind != "deleted" {
		t.Fatal("tracked deletion lost", entry)
	}
	if entry := findPath(t, repository, "script.sh"); entry.Mode&0100 == 0 {
		t.Fatal("executable mode lost", entry)
	}
	if entry := findPath(t, repository, "link"); entry.Kind != "symlink" || entry.Digest != store.Digest([]byte("untracked.txt")) {
		t.Fatal("symlink target lost", entry)
	}
	var sameIndex IndexEntry
	for _, entry := range repository.IndexEntries {
		if entry.Path == "same.txt" {
			sameIndex = entry
		}
	}
	staged, err := os.ReadFile(filepath.Join(checkpointStore.Dir, "blobs", sameIndex.BlobDigest))
	if err != nil || string(staged) != "staged\n" {
		t.Fatal("staged bytes lost", string(staged), err)
	}
}

func TestDescriptorRelativeObjectRecoverySupportsGitFormatsAndConflictStages(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "repository")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			init := exec.Command("git", "init", "-q", "-b", "main", "--object-format="+format, root)
			init.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
			if output, err := init.CombinedOutput(); err != nil {
				if format == "sha256" {
					t.Skipf("installed Git lacks SHA-256 repository support: %s", output)
				}
				t.Fatal(err, string(output))
			}
			write(t, filepath.Join(root, "same.txt"), []byte("base\n"), 0600)
			git(t, "-C", root, "add", "same.txt")
			git(t, "-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "base")
			var expectedOIDs []string
			if format == "sha1" {
				var stageLines strings.Builder
				for stage, content := range []string{"base stage\n", "ours stage\n", "theirs stage\n"} {
					path := filepath.Join(base, fmt.Sprintf("stage-%d", stage+1))
					write(t, path, []byte(content), 0600)
					oid := strings.TrimSpace(string(git(t, "-C", root, "hash-object", "-w", path)))
					expectedOIDs = append(expectedOIDs, oid)
					fmt.Fprintf(&stageLines, "100644 %s %d\tsame.txt\n", oid, stage+1)
				}
				var lines strings.Builder
				fmt.Fprintf(&lines, "0 %s\tsame.txt\n", strings.Repeat("0", len(expectedOIDs[0])))
				lines.WriteString(stageLines.String())
				gitInput(t, lines.String(), "-C", root, "update-index", "--index-info")
			} else {
				write(t, filepath.Join(root, "same.txt"), []byte("unique sha256 staged bytes\n"), 0600)
				git(t, "-C", root, "add", "same.txt")
				expectedOIDs = []string{strings.TrimSpace(string(git(t, "-C", root, "rev-parse", ":same.txt")))}
			}
			identity, err := workspace.Inspect(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			checkpointStore, err := Open(filepath.Join(base, "private", "checkpoints"))
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := checkpointStore.Capture(context.Background(), "object-set", 1, []RepositorySpec{{ID: "repo", Root: root, Identity: identity, Exclusions: []string{".git"}, UntrackedScope: []string{"**"}}})
			if err != nil {
				t.Fatal(err)
			}
			repository := manifest.Repositories[0]
			if format == "sha1" && len(repository.IndexEntries) != 3 {
				t.Fatalf("conflict stages were not captured: %#v", repository.IndexEntries)
			}
			for _, oid := range expectedOIDs {
				if err := os.Remove(filepath.Join(identity.CommonGitPath, "objects", oid[:2], oid[2:])); err != nil {
					t.Fatal(err)
				}
			}
			manager := &Manager{Store: checkpointStore}
			if err := manager.restoreIndexObjects(context.Background(), repository); err != nil {
				t.Fatal(err)
			}
			for _, oid := range expectedOIDs {
				if err := exec.Command("git", "-C", root, "cat-file", "-e", oid+"^{blob}").Run(); err != nil {
					t.Fatal("restored object is unusable", oid, err)
				}
			}
		})
	}
}

func TestCaptureSetIsAllRepositoriesOrNoneVerified(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	parentIdentity := repositoryFixture(t, parent)
	child := filepath.Join(parent, "nested")
	childIdentity := repositoryFixture(t, child)
	checkpointStore, err := Open(filepath.Join(base, "private", "checkpoints"))
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("fail after first repository")
	checkpointStore.Fault = func(point string) error {
		if point == "after_repository:child" {
			return injected
		}
		return nil
	}
	_, err = checkpointStore.Capture(context.Background(), "failed-set", 1, []RepositorySpec{
		{ID: "parent", Root: parent, Identity: parentIdentity, Exclusions: []string{".git", "nested"}, UntrackedScope: []string{"**"}},
		{ID: "child", Root: child, Identity: childIdentity, Exclusions: []string{".git"}, UntrackedScope: []string{"**"}},
	})
	if !errors.Is(err, injected) {
		t.Fatal("failure point not reached", err)
	}
	if _, err := os.Stat(filepath.Join(checkpointStore.Dir, "sets", "failed-set")); !os.IsNotExist(err) {
		t.Fatal("failed checkpoint became a verified set", err)
	}
	if _, err := os.Stat(filepath.Join(checkpointStore.Dir, "incomplete", "failed-set", "failure.json")); err != nil {
		t.Fatal("incomplete capture was not retained", err)
	}
	if content, err := os.ReadFile(filepath.Join(parent, "same.txt")); err != nil || string(content) != "unstaged\n" {
		t.Fatal("capture failure modified source checkout", string(content), err)
	}
}

func TestCheckpointCorruptionFailsClosed(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repository")
	identity := repositoryFixture(t, root)
	checkpointStore, err := Open(filepath.Join(base, "private", "checkpoints"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := checkpointStore.Capture(context.Background(), "corrupt-set", 1, []RepositorySpec{{ID: "repo", Root: root, Identity: identity, Exclusions: []string{".git"}, UntrackedScope: []string{"**"}}})
	if err != nil {
		t.Fatal(err)
	}
	entry := findPath(t, findRepository(t, manifest, "repo"), "same.txt")
	if err := os.WriteFile(filepath.Join(checkpointStore.Dir, "blobs", entry.Digest), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkpointStore.Verify(context.Background(), manifest); err == nil {
		t.Fatal("corrupt recovery blob verified")
	}
}
