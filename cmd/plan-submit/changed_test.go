package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestParseNameStatus covers each status git can emit for a tracked file, with
// the two that matter most: a rename must submit the NEW path and retire the
// old, and a copy must submit the new path without retiring anything.
func TestParseNameStatus(t *testing.T) {
	out := strings.Join([]string{
		"A\tnew.yaml",
		"M\tedited.yaml",
		"D\tgone.yaml",
		"T\ttypechanged.yaml",
		"R100\told-name.yaml\tnew-name.yaml",
		"C075\tsource.yaml\tcopy.yaml",
		"", // trailing blank line, as git emits
	}, "\n")

	changed, deleted := parseNameStatus(out)
	sort.Strings(changed)
	sort.Strings(deleted)

	wantChanged := []string{"copy.yaml", "edited.yaml", "new-name.yaml", "new.yaml", "typechanged.yaml"}
	wantDeleted := []string{"gone.yaml", "old-name.yaml"}
	if strings.Join(changed, ",") != strings.Join(wantChanged, ",") {
		t.Errorf("changed = %v, want %v", changed, wantChanged)
	}
	if strings.Join(deleted, ",") != strings.Join(wantDeleted, ",") {
		t.Errorf("deleted = %v, want %v", deleted, wantDeleted)
	}
}

// TestSelectChanged asserts the intersection keeps discover()'s ordering, drops
// untouched plans, and never matches a subdirectory path onto a top-level file
// of the same basename (discover is deliberately flat).
func TestSelectChanged(t *testing.T) {
	all := []string{"plans/a.yaml", "plans/b.yaml", "plans/c.yaml"}

	got := selectChanged(all, []string{"c.yaml", "a.yaml"})
	if strings.Join(got, ",") != "plans/a.yaml,plans/c.yaml" {
		t.Errorf("selectChanged = %v, want discover order [a c]", got)
	}
	if got := selectChanged(all, nil); len(got) != 0 {
		t.Errorf("no changes should select nothing, got %v", got)
	}
	if got := selectChanged(all, []string{"sub/a.yaml"}); len(got) != 0 {
		t.Errorf("a subdirectory path must not match a top-level file, got %v", got)
	}
	if got := selectChanged(all, []string{"./b.yaml"}); strings.Join(got, ",") != "plans/b.yaml" {
		t.Errorf("unclean path should still match, got %v", got)
	}
}

// TestChangedPlanFiles exercises the real git plumbing against a scratch repo:
// one edited plan, one added, one deleted, one untouched. The untouched file
// staying out of the result IS the fix — it is what stops a merge re-asserting
// (and so resurrecting) every other plan in the catalog.
func TestChangedPlanFiles(t *testing.T) {
	root := initScratchRepo(t)
	plansDir := filepath.Join(root, "plans")

	writeFile(t, plansDir, "untouched.yaml", "a: 1\n")
	writeFile(t, plansDir, "edited.yaml", "b: 1\n")
	writeFile(t, plansDir, "deleted.yaml", "c: 1\n")
	gitMust(t, root, "add", "-A")
	gitMust(t, root, "commit", "-qm", "base")

	writeFile(t, plansDir, "edited.yaml", "b: 2\n")
	writeFile(t, plansDir, "added.yaml", "d: 1\n")
	if err := os.Remove(filepath.Join(plansDir, "deleted.yaml")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	gitMust(t, root, "add", "-A")
	gitMust(t, root, "commit", "-qm", "the merge")

	changed, deleted, err := changedPlanFiles(plansDir, "HEAD^")
	if err != nil {
		t.Fatalf("changedPlanFiles: %v", err)
	}
	sort.Strings(changed)
	if strings.Join(changed, ",") != "added.yaml,edited.yaml" {
		t.Errorf("changed = %v, want [added.yaml edited.yaml]", changed)
	}
	if strings.Join(deleted, ",") != "deleted.yaml" {
		t.Errorf("deleted = %v, want [deleted.yaml]", deleted)
	}

	// End to end: discover() ∩ changed must exclude the untouched plan.
	all, err := discover(plansDir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(all) != 3 { // untouched, edited, added
		t.Fatalf("discover = %v, want 3 files", all)
	}
	scoped := selectChanged(all, changed)
	for _, f := range scoped {
		if filepath.Base(f) == "untouched.yaml" {
			t.Errorf("untouched plan must not be submitted: %v", scoped)
		}
	}
	if len(scoped) != 2 {
		t.Errorf("scoped = %v, want 2 files", scoped)
	}
}

// TestChangedPlanFiles_NoPlanChanges asserts a merge that touches nothing under
// plans/ selects nothing — the common case once this lands, and the one that
// must not fall back to submitting everything.
func TestChangedPlanFiles_NoPlanChanges(t *testing.T) {
	root := initScratchRepo(t)
	plansDir := filepath.Join(root, "plans")

	writeFile(t, plansDir, "a.yaml", "a: 1\n")
	gitMust(t, root, "add", "-A")
	gitMust(t, root, "commit", "-qm", "base")

	writeFile(t, root, "README.md", "docs only\n")
	gitMust(t, root, "add", "-A")
	gitMust(t, root, "commit", "-qm", "docs")

	changed, deleted, err := changedPlanFiles(plansDir, "HEAD^")
	if err != nil {
		t.Fatalf("changedPlanFiles: %v", err)
	}
	if len(changed) != 0 || len(deleted) != 0 {
		t.Errorf("docs-only merge: changed=%v deleted=%v, want both empty", changed, deleted)
	}
}

// TestResolveBase_NoParent asserts an unresolvable base is a hard error naming
// -all, not a silent full submit (which would resurrect) and not a silent empty
// submit (which would drop a genuinely new plan).
func TestResolveBase_NoParent(t *testing.T) {
	root := initScratchRepo(t)
	plansDir := filepath.Join(root, "plans")
	writeFile(t, plansDir, "a.yaml", "a: 1\n")
	gitMust(t, root, "add", "-A")
	gitMust(t, root, "commit", "-qm", "root commit, no parent")

	_, _, err := changedPlanFiles(plansDir, "HEAD^")
	if err == nil {
		t.Fatal("expected an error diffing against a root commit's parent")
	}
	if !strings.Contains(err.Error(), "-all") {
		t.Errorf("error must point at the -all escape hatch, got: %v", err)
	}
}

func initScratchRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH — skipping")
	}
	root := t.TempDir()
	gitMust(t, root, "init", "-q")
	gitMust(t, root, "config", "user.email", "test@example.com")
	gitMust(t, root, "config", "user.name", "plan-submit test")
	return root
}

func gitMust(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := gitOut(dir, args...); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
