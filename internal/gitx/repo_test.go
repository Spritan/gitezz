package gitx

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func initTempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("README"); err != nil {
		t.Fatal(err)
	}
	_, err = w.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Ensure HEAD is on a named branch (go-git defaults to master).
	head, err := r.Head()
	if err != nil {
		t.Fatal(err)
	}
	if !head.Name().IsBranch() {
		t.Fatal("expected branch HEAD")
	}
	return dir
}

func TestStatusCommitDirty(t *testing.T) {
	dir := initTempRepo(t)
	if IsDirty(dir) {
		t.Fatal("expected clean")
	}
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsDirty(dir) {
		t.Fatal("expected dirty")
	}
	files, err := StatusPorcelain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("expected file changes")
	}
	if _, err := Add(dir, "README"); err != nil {
		t.Fatal(err)
	}
	out, err := Commit(dir, "update readme")
	if err != nil {
		t.Fatalf("commit: %v (%s)", err, out)
	}
	if IsDirty(dir) {
		t.Fatal("expected clean after commit")
	}
	if CurrentBranch(dir) == "?" {
		t.Fatal("branch")
	}
	logs, err := Log(dir, 5)
	if err != nil || len(logs) < 2 {
		t.Fatalf("log: %v %#v", err, logs)
	}
}

func TestDiscardUnstage(t *testing.T) {
	dir := initTempRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(dir, "README"); err != nil {
		t.Fatal(err)
	}
	if _, err := Unstage(dir, "README"); err != nil {
		t.Fatal(err)
	}
	files, err := StatusPorcelain(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.Path == "README" && f.Work {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unstaged worktree change: %#v", files)
	}
	if _, err := Discard(dir, "README"); err != nil {
		t.Fatal(err)
	}
	if IsDirty(dir) {
		t.Fatal("expected clean after discard")
	}
}

func TestSwitchAndSync(t *testing.T) {
	dir := initTempRepo(t)
	r, err := openRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := r.Head()
	if err != nil {
		t.Fatal(err)
	}
	base := head.Name().Short()

	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	err = w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("feature"),
		Create: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("feat.txt"); err != nil {
		t.Fatal(err)
	}
	_, err = w.Commit("feat", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := Switch(dir, base)
	if err != nil {
		t.Fatalf("switch: %v (%s)", err, out)
	}
	if CurrentBranch(dir) != base {
		t.Fatalf("want %s got %s", base, CurrentBranch(dir))
	}
	branches := Branches(dir)
	if len(branches) < 2 {
		t.Fatalf("branches: %v", branches)
	}

	// Fake upstream: create remote-tracking ref at older commit.
	if _, err := r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{"file:///tmp/none"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateBranch(&config.Branch{Name: base, Remote: "origin", Merge: plumbing.NewBranchReferenceName(base)}); err != nil {
		t.Fatal(err)
	}
	head, _ = r.Head()
	parent, err := r.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if len(parent.ParentHashes) == 0 {
		// make another commit so we can be ahead
		if err := os.WriteFile(filepath.Join(dir, "README"), []byte("v2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Add(dir, "README"); err != nil {
			t.Fatal(err)
		}
		if _, err := Commit(dir, "v2"); err != nil {
			t.Fatal(err)
		}
		head, _ = r.Head()
		parent, _ = r.CommitObject(head.Hash())
	}
	upHash := parent.ParentHashes[0]
	ref := plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", base), upHash)
	if err := r.Storer.SetReference(ref); err != nil {
		t.Fatal(err)
	}
	sync := UpstreamSync(dir)
	if !sync.HasUpstream {
		t.Fatal("expected upstream")
	}
	if sync.Ahead < 1 {
		t.Fatalf("expected ahead, got %#v", sync)
	}
}

func TestSummarizePullGoGit(t *testing.T) {
	if SummarizePull("Already up to date.") != "Already up to date" {
		t.Fatal(SummarizePull("Already up to date."))
	}
	if SummarizePull("Fast-forward") != "Fast-forward" {
		t.Fatal(SummarizePull("Fast-forward"))
	}
}

func TestClassifyWrappedErrors(t *testing.T) {
	if Classify(OpPush, "non-fast-forward: updates were rejected, fetch first: x") != KindPushNonFF {
		t.Fatal("nonff")
	}
	if Classify(OpPush, "authentication failed: denied") != KindPushAuth {
		t.Fatal("auth")
	}
	if Classify(OpSwitch, "your local changes to the following files would be overwritten by checkout: x") != KindSwitchDirty {
		t.Fatal("switch")
	}
	if Classify(OpCommit, "nothing to commit, working tree clean: x") != KindCommitEmpty {
		t.Fatal("commit")
	}
}

func TestCommitEmpty(t *testing.T) {
	dir := initTempRepo(t)
	_, err := Commit(dir, "noop")
	if err == nil {
		t.Fatal("expected empty commit error")
	}
	if Classify(OpCommit, err.Error()) != KindCommitEmpty {
		t.Fatal(Classify(OpCommit, err.Error()))
	}
}

func TestLiveFetchAuthSmoke(t *testing.T) {
	if os.Getenv("GITEZZ_LIVE") != "1" {
		t.Skip("set GITEZZ_LIVE=1 to run")
	}
	paths := []string{
		"/home/spritan/Projects/hood/centrum/itsm/Hood-itsm-be",
		"/home/spritan/Projects/hood/centrum/itsm/hood-nms",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Skip("hood repos not present")
		}
	}
	for _, p := range paths {
		out, err := Fetch(p)
		if err != nil {
			t.Fatalf("%s: %v (%s)", p, err, out)
		}
		t.Log(p, out)
	}
}
