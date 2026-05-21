package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// canon mirrors Resolve's symlink canonicalization so assertions hold on
// platforms where the temp dir is itself a symlink (e.g. macOS /var).
func canon(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("evalsymlinks %s: %v", path, err)
	}
	return resolved
}

func writeGitConfig(t *testing.T, gitRoot, contents string) {
	t.Helper()
	dir := filepath.Join(gitRoot, ".git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if contents != "" {
		if err := os.WriteFile(filepath.Join(dir, "config"), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveFromSubdirYieldsSameIdentity(t *testing.T) {
	root := t.TempDir()
	writeGitConfig(t, root, "")
	sub := filepath.Join(root, "cmd", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	fromRoot, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	fromSub, err := Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}

	if fromRoot.Identity != fromSub.Identity {
		t.Fatalf("identity differs between root (%q) and subdir (%q)", fromRoot.Identity, fromSub.Identity)
	}
	if fromSub.Root != canon(t, root) {
		t.Fatalf("subdir resolved root = %q, want %q", fromSub.Root, canon(t, root))
	}
	if fromRoot.Identity != canon(t, root) {
		t.Fatalf("no-remote identity = %q, want git root %q", fromRoot.Identity, canon(t, root))
	}
}

func TestResolvePrefersNormalizedRemote(t *testing.T) {
	root := t.TempDir()
	writeGitConfig(t, root, `[remote "origin"]
	url = git@github.com:Acme/Mnemo.git
`)

	// A non-existent subdir still resolves: Resolve abs-joins then walks up.
	got, err := Resolve(filepath.Join(root, "internal", "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "github.com/acme/mnemo"; got.Identity != want {
		t.Fatalf("identity = %q, want %q", got.Identity, want)
	}
}

func TestResolveNoGitFallsBackToDir(t *testing.T) {
	dir := t.TempDir()
	got, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != canon(t, dir) || got.Identity != canon(t, dir) {
		t.Fatalf("non-git resolve = %+v, want root/identity %q", got, canon(t, dir))
	}
}
