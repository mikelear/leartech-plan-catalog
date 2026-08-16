package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Diff-scoping: which plans does THIS merge submit?
//
// plan-api de-duplicates on the LIVE CRD (apiserver AlreadyExists -> HTTP 409),
// not on a durable record of what was submitted. So submitting the whole
// plans/ dir on every merge means a Plan deleted from a cluster is recreated
// (as a paused proposal) by the next unrelated merge — the repo behaves as a
// desired-state mirror rather than a history. Scoping each release to the files
// its own merge commit touched makes deletion stick: the catalog records that a
// Plan once existed without perpetually re-asserting it.
//
// The full-dir behaviour survives behind -all, which is the deliberate backfill
// path (e.g. seeding a fresh cluster from the repo).

// gitOut runs a git subcommand rooted at dir and returns trimmed stdout.
//
// safe.directory=* : the release Job's container user is not the owner of the
// Tekton workspace, which otherwise trips git's dubious-ownership refusal.
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "safe.directory=*"}, args...)...) //nolint:gosec // fixed argv, no shell
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// changedPlanFiles returns the plan files under dir that base..HEAD touched, as
// names RELATIVE to dir (e.g. "agent-selfwrite-guard.yaml"), split into files
// still present in the tree and files the merge deleted.
//
// base defaults to HEAD^ = main's previous tip, so on a merge commit this is
// exactly what the merged PR brought in (and on a squash-merge or a direct push,
// exactly that commit). Deleted files are returned so the caller can report
// them: this tool never deletes a Plan CRD, and a silent drop would hide the
// one case where repo and cluster legitimately diverge.
func changedPlanFiles(dir, base string) (changed, deleted []string, err error) {
	if err := resolveBase(dir, base); err != nil {
		return nil, nil, err
	}
	// --relative: emit paths relative to dir, so this works regardless of the
	// caller's cwd or whether -dir was given as an absolute path.
	// -M: a rename reports the new path as changed and the old as deleted.
	out, err := gitOut(dir, "diff", "--name-status", "-M", "--relative", base, "HEAD", "--", ".")
	if err != nil {
		return nil, nil, err
	}
	changed, deleted = parseNameStatus(out)
	return changed, deleted, nil
}

// resolveBase verifies base names a commit, deepening a shallow clone once
// before giving up. A shallow clone with no parent is a real ambiguity — we
// cannot tell "nothing changed" from "cannot see what changed" — so it is a
// hard error pointing at -all rather than a silent full or empty submit.
func resolveBase(dir, base string) error {
	if _, err := gitOut(dir, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err == nil {
		return nil
	}
	// Best-effort: a depth-1 clone has no parent commit to diff against.
	_, _ = gitOut(dir, "fetch", "--deepen=2", "--quiet")
	if _, err := gitOut(dir, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err != nil {
		return fmt.Errorf("cannot resolve %s (shallow clone, or a root commit with no parent): "+
			"re-run with -all to submit every plan in %s, or clone with more history", base, dir)
	}
	return nil
}

// parseNameStatus splits `git diff --name-status -M` output into files that
// exist after the change and files that no longer do.
func parseNameStatus(out string) (changed, deleted []string) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 2 || fields[0] == "" {
			continue
		}
		status := fields[0]
		switch {
		case strings.HasPrefix(status, "R"), strings.HasPrefix(status, "C"):
			// Rename/copy: "R100\told\tnew". The new path is submittable; a
			// rename also retires the old one (a copy does not).
			if len(fields) < 3 {
				continue
			}
			changed = append(changed, fields[2])
			if strings.HasPrefix(status, "R") {
				deleted = append(deleted, fields[1])
			}
		case strings.HasPrefix(status, "D"):
			deleted = append(deleted, fields[1])
		default:
			// A, M, T and anything else that leaves a file in the tree.
			changed = append(changed, fields[1])
		}
	}
	return changed, deleted
}

// selectChanged intersects discover()'s submittable files (dir-joined paths)
// with the changed set (names relative to dir), preserving discover's order.
//
// discover() is deliberately flat — only the top level of dir — so a changed
// path containing a separator is in a subdirectory and can never be one of its
// files, regardless of basename collisions.
func selectChanged(all, changedRel []string) []string {
	set := make(map[string]bool, len(changedRel))
	for _, c := range changedRel {
		c = filepath.ToSlash(filepath.Clean(c))
		if strings.Contains(c, "/") {
			continue
		}
		set[c] = true
	}
	var out []string
	for _, f := range all {
		if set[filepath.Base(f)] {
			out = append(out, f)
		}
	}
	return out
}
