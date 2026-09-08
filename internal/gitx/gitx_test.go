package gitx

import "testing"

func TestClassifySwitchDirty(t *testing.T) {
	out := "error: Your local changes to the following files would be overwritten by checkout:\n\tair.toml"
	if Classify(OpSwitch, out) != KindSwitchDirty {
		t.Fatal(Classify(OpSwitch, out))
	}
}

func TestClassifyPullConflict(t *testing.T) {
	out := "CONFLICT (content): Merge conflict in foo.go\nAutomatic merge failed; fix conflicts and then commit the result."
	if Classify(OpPull, out) != KindPullConflict {
		t.Fatal(Classify(OpPull, out))
	}
}

func TestClassifyPullOverwrite(t *testing.T) {
	out := "error: Your local changes to the following files would be overwritten by merge:\n\tMakefile"
	if Classify(OpPull, out) != KindPullOverwrite {
		t.Fatal(Classify(OpPull, out))
	}
}

func TestClassifyPushNonFF(t *testing.T) {
	out := "! [rejected] main -> main (non-fast-forward)\nerror: failed to push some refs"
	if Classify(OpPush, out) != KindPushNonFF {
		t.Fatal(Classify(OpPush, out))
	}
}

func TestClassifyPushAuth(t *testing.T) {
	out := "Permission denied (publickey)."
	if Classify(OpPush, out) != KindPushAuth {
		t.Fatal(Classify(OpPush, out))
	}
}

func TestClassifyCommitEmpty(t *testing.T) {
	out := "nothing to commit, working tree clean"
	if Classify(OpCommit, out) != KindCommitEmpty {
		t.Fatal(Classify(OpCommit, out))
	}
}

func TestSwitchBlockedByChanges(t *testing.T) {
	out := "error: Your local changes to the following files would be overwritten by checkout:\n\tair.toml"
	if !SwitchBlockedByChanges(out) {
		t.Fatal("expected blocked")
	}
	if SwitchBlockedByChanges("Already on 'main'") {
		t.Fatal("should not block")
	}
}

func TestSummarizePull(t *testing.T) {
	if SummarizePull("Already up to date.") != "Already up to date" {
		t.Fatal(SummarizePull("Already up to date."))
	}
	out := "Updating abc..def\nFast-forward\n foo | 2 +-\n 1 file changed, 1 insertion(+)"
	got := SummarizePull(out)
	if got != "Fast-forward · 1 file" {
		t.Fatal(got)
	}
}
