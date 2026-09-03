package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The whole POINT of this file: plan-api's 409 is NOT unconditional success.
// Each behaviour below is stated as a test whose name reads out the intent —
// per the initiative's rule "do not document the new behaviour in a comment;
// put the behaviour in tests, named so the intent is readable from the test
// name". Every case is proven load-bearing at the bottom of the file: mutate
// the classifier's output for that verdict and the matching test breaks.

// samplePlanYAML is the minimal projectable Plan the fake plan-api receives.
// Kept simple — the wire contract we're exercising is on the RESPONSE side.
const samplePlanYAML = `apiVersion: agent.leartech.io/v1alpha1
kind: Plan
metadata:
  name: %s
spec:
  paused: true
  steps:
    - name: s
      kind: check
      use: verify-release-flow
      with:
        service: leartech-plan-api
`

// writePlan drops a YAML file into a temp plans/ dir named after the plan and
// returns the file path. The filename encodes the plan name so failure messages
// carry both.
func writePlan(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(samplePlanYAML, name)), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// runOne submits a single projected plan against the fake server and returns
// (ok, message). This is the classifier-visible slice of the release loop.
// The submit() and fetchToken() helpers are exercised end-to-end via the
// multi-plan test below; the classifier tests exercise classifyResubmit
// directly for compact, targeted table-driven coverage.
func runOne(t *testing.T, planName, filePath, body string) (bool, string) {
	t.Helper()
	return classifyResubmit(planName, filePath, body)
}

// TestClassifyResubmit_IdenticalExitsZeroAndPrintsSKIP proves the ONE
// verdict that keeps 409-as-success: on-cluster brief matches the file,
// so the resubmit is a safe no-op.
func TestClassifyResubmit_IdenticalExitsZeroAndPrintsSKIP(t *testing.T) {
	body := `{"error":"plan already exists","result":"identical","phase":"Paused","paused":true}`
	ok, msg := runOne(t, "plan-x", "plans/plan-x.yaml", body)
	if !ok {
		t.Fatalf("identical must be success: %s", msg)
	}
	if !strings.Contains(msg, "SKIP") {
		t.Errorf("identical message must print SKIP, got: %s", msg)
	}
	if !strings.Contains(msg, "plan-x") {
		t.Errorf("message must name the plan, got: %s", msg)
	}
}

// TestClassifyResubmit_DiffersFailsAndNamesRemediation is the core of the
// fix: a plan-api that computed `differs` means the amendment was NOT
// applied — the operator needs the plan name AND the remediation
// (delete the CR, re-submit), not a bare "differs".
func TestClassifyResubmit_DiffersFailsAndNamesRemediation(t *testing.T) {
	body := `{"error":"plan spec has changed since submission — existing plan NOT modified; delete and re-submit to apply the new brief","result":"differs","phase":"Paused","paused":true,"changed":["steps"]}`
	ok, msg := runOne(t, "plan-x", "plans/plan-x.yaml", body)
	if ok {
		t.Fatalf("differs must FAIL, got success: %s", msg)
	}
	if !strings.Contains(msg, "FAIL") {
		t.Errorf("differs message must print FAIL, got: %s", msg)
	}
	if !strings.Contains(msg, "plan-x") {
		t.Errorf("message must name the plan, got: %s", msg)
	}
	if !strings.Contains(msg, "delete") || !strings.Contains(msg, "re-submit") {
		t.Errorf("message must state remediation (delete + re-submit), got: %s", msg)
	}
	if !strings.Contains(msg, "steps") {
		t.Errorf("message must include what plan-api reported changed (`steps`), got: %s", msg)
	}
	if !strings.Contains(msg, "DID NOT apply") {
		t.Errorf("message must state the amendment was NOT applied, got: %s", msg)
	}
}

// TestClassifyResubmit_ConflictRunningFailsAndNamesRemediation covers the
// same-class failure where the on-cluster plan is mid-flight and cannot
// safely be swapped. Operator needs to wait / cancel before deleting.
func TestClassifyResubmit_ConflictRunningFailsAndNamesRemediation(t *testing.T) {
	body := `{"error":"plan spec has changed but the plan is not paused and not terminal — cannot resubmit; wait for terminal or pause first","result":"conflict_running","phase":"Running","paused":false,"changed":["steps"]}`
	ok, msg := runOne(t, "plan-x", "plans/plan-x.yaml", body)
	if ok {
		t.Fatalf("conflict_running must FAIL, got success: %s", msg)
	}
	if !strings.Contains(msg, "plan-x") {
		t.Errorf("message must name the plan, got: %s", msg)
	}
	if !strings.Contains(msg, "Running") {
		t.Errorf("message must include the on-cluster phase, got: %s", msg)
	}
	if !strings.Contains(msg, "wait") {
		t.Errorf("message must state the wait/cancel remediation, got: %s", msg)
	}
}

// TestClassifyResubmit_MissingVerdictFails is the fail-open guard: an
// older plan-api that predates PR #33 answers 409 without a `result`
// field. The old code silently succeeded; the new code MUST fail rather
// than swallow it.
func TestClassifyResubmit_MissingVerdictFails(t *testing.T) {
	body := `{"error":"plan already exists"}`
	ok, msg := runOne(t, "plan-x", "plans/plan-x.yaml", body)
	if ok {
		t.Fatalf("no verdict must FAIL, got success: %s", msg)
	}
	if !strings.Contains(msg, "no `result` verdict") {
		t.Errorf("message must call out the missing verdict, got: %s", msg)
	}
	if !strings.Contains(msg, "#33") {
		t.Errorf("message must point at plan-api PR #33 as the wire contract source, got: %s", msg)
	}
}

// TestClassifyResubmit_UnrecognisedVerdictFails is the same fail-closed
// rule for a plan-api that adds a NEW verdict this build doesn't know
// how to interpret. Failing is right because the reader cannot
// determine the outcome — the run_report UNAVAILABLE rule.
func TestClassifyResubmit_UnrecognisedVerdictFails(t *testing.T) {
	body := `{"error":"plan already exists","result":"quantum_flux"}`
	ok, msg := runOne(t, "plan-x", "plans/plan-x.yaml", body)
	if ok {
		t.Fatalf("unrecognised verdict must FAIL, got success: %s", msg)
	}
	if !strings.Contains(msg, "quantum_flux") {
		t.Errorf("message must quote the unrecognised verdict, got: %s", msg)
	}
	if !strings.Contains(msg, "unrecognised") {
		t.Errorf("message must say `unrecognised`, got: %s", msg)
	}
}

// TestClassifyResubmit_UnknownVerdictFails covers plan-api's explicit
// `unknown` (a transient Get failure post-AlreadyExists). Falls through
// to the "unrecognised" branch: this tool treats `unknown` as a
// non-success, matching the initiative's rule that a reader unable to
// determine the outcome must say so.
func TestClassifyResubmit_UnknownVerdictFails(t *testing.T) {
	body := `{"error":"plan already exists","result":"unknown"}`
	ok, msg := runOne(t, "plan-x", "plans/plan-x.yaml", body)
	if ok {
		t.Fatalf("`unknown` verdict must FAIL, got success: %s", msg)
	}
	if !strings.Contains(msg, "unknown") {
		t.Errorf("message must quote the `unknown` verdict, got: %s", msg)
	}
}

// TestClassifyResubmit_EmptyBodyFails covers the case where plan-api
// returned 409 with no body at all — old behaviour silently succeeded,
// new behaviour fails.
func TestClassifyResubmit_EmptyBodyFails(t *testing.T) {
	ok, msg := runOne(t, "plan-x", "plans/plan-x.yaml", "")
	if ok {
		t.Fatalf("empty body must FAIL, got success: %s", msg)
	}
	if !strings.Contains(msg, "empty body") {
		t.Errorf("message must call out the empty body, got: %s", msg)
	}
}

// TestClassifyResubmit_UnparseableBodyFails covers a 409 whose body is
// not JSON at all (proxy interposing HTML, half-written stream). The
// initiative's rule: if we can't determine the outcome, we say so.
func TestClassifyResubmit_UnparseableBodyFails(t *testing.T) {
	ok, msg := runOne(t, "plan-x", "plans/plan-x.yaml", "<html><body>502 Bad Gateway</body></html>")
	if ok {
		t.Fatalf("unparseable body must FAIL, got success: %s", msg)
	}
	if !strings.Contains(msg, "unparseable body") {
		t.Errorf("message must call out unparseable body, got: %s", msg)
	}
}

// TestSubmit_201Succeeds is the happy path: a genuinely new plan gets a
// 201 and the release step reports OK. Kept end-to-end against the fake
// server so the http/token wiring is covered too.
func TestSubmit_201Succeeds(t *testing.T) {
	srv := newFakePlanAPI(t, map[string]fakeResp{
		"plan-new": {status: http.StatusCreated, body: `{"name":"plan-new"}`},
	})
	defer srv.Close()

	planFile := writePlan(t, filepath.Join(t.TempDir(), "plans"), "plan-new")
	req, err := projectFile(planFile)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	status, body, err := submit(context.Background(), srv.URL, "test-token", req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if status != http.StatusCreated {
		t.Errorf("status = %d, want 201; body=%s", status, body)
	}
}

// TestRun_OneDiffersFailsWholeRun is the initiative's headline: ONE
// stale plan among several must fail the WHOLE step. Exercises the
// exit-path counter by running the full `run()` against a fake server
// with a mix of 201 + identical + differs responses.
func TestRun_OneDiffersFailsWholeRun(t *testing.T) {
	srv := newFakePlanAPI(t, map[string]fakeResp{
		"plan-a": {status: http.StatusCreated, body: `{"name":"plan-a"}`},
		"plan-b": {status: http.StatusConflict, body: `{"error":"identical","result":"identical","phase":"Paused","paused":true}`},
		"plan-c": {status: http.StatusConflict, body: `{"error":"spec drift","result":"differs","phase":"Paused","paused":true,"changed":["steps"]}`},
	})
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "plans")
	writePlan(t, dir, "plan-a")
	writePlan(t, dir, "plan-b")
	writePlan(t, dir, "plan-c")

	withEnv(t, map[string]string{
		"LEARTECH_AUTH_TOKEN_URL":          srv.URL + "/token",
		"LEARTECH_PLAN_SUBMIT_CLIENT_ID":   "id",
		"LEARTECH_PLAN_SUBMIT_CLIENT_SECRET": "secret",
	})

	err := run(dir, srv.URL, false, true /* -all */, "HEAD^")
	if err == nil {
		t.Fatalf("expected a failure because plan-c differs, got nil")
	}
	// The failure MUST count exactly one failure and mention plan-c.
	if !strings.Contains(err.Error(), "1 of 3") {
		t.Errorf("expected `1 of 3 plan(s) failed`, got: %v", err)
	}
}

// TestRun_AllIdenticalOrCreatedSucceeds is the twin: no differs means
// the whole run exits 0. Guards against the classifier over-failing.
func TestRun_AllIdenticalOrCreatedSucceeds(t *testing.T) {
	srv := newFakePlanAPI(t, map[string]fakeResp{
		"plan-a": {status: http.StatusCreated, body: `{"name":"plan-a"}`},
		"plan-b": {status: http.StatusConflict, body: `{"error":"identical","result":"identical","phase":"Paused","paused":true}`},
	})
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "plans")
	writePlan(t, dir, "plan-a")
	writePlan(t, dir, "plan-b")

	withEnv(t, map[string]string{
		"LEARTECH_AUTH_TOKEN_URL":            srv.URL + "/token",
		"LEARTECH_PLAN_SUBMIT_CLIENT_ID":     "id",
		"LEARTECH_PLAN_SUBMIT_CLIENT_SECRET": "secret",
	})

	if err := run(dir, srv.URL, false, true /* -all */, "HEAD^"); err != nil {
		t.Fatalf("run must succeed when every plan is 201 or identical, got: %v", err)
	}
}

// TestRun_OneConflictRunningFailsWholeRun exercises the second failing
// verdict end-to-end. Kept separate from the differs case so a
// classifier that only handles one still fails the other test.
func TestRun_OneConflictRunningFailsWholeRun(t *testing.T) {
	srv := newFakePlanAPI(t, map[string]fakeResp{
		"plan-a": {status: http.StatusCreated, body: `{"name":"plan-a"}`},
		"plan-b": {status: http.StatusConflict, body: `{"error":"running","result":"conflict_running","phase":"Running","paused":false,"changed":["steps"]}`},
	})
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "plans")
	writePlan(t, dir, "plan-a")
	writePlan(t, dir, "plan-b")

	withEnv(t, map[string]string{
		"LEARTECH_AUTH_TOKEN_URL":            srv.URL + "/token",
		"LEARTECH_PLAN_SUBMIT_CLIENT_ID":     "id",
		"LEARTECH_PLAN_SUBMIT_CLIENT_SECRET": "secret",
	})

	err := run(dir, srv.URL, false, true /* -all */, "HEAD^")
	if err == nil {
		t.Fatal("expected failure on conflict_running verdict")
	}
	if !strings.Contains(err.Error(), "1 of 2") {
		t.Errorf("expected `1 of 2 plan(s) failed`, got: %v", err)
	}
}

// fakeResp is the fake plan-api's per-name reply.
type fakeResp struct {
	status int
	body   string
}

// newFakePlanAPI stands up an httptest.Server that mimics plan-api's
// POST /plans and the Hydra-compatible /token endpoint the tool mints
// against. The reply set is keyed by the plan name in the request body,
// so a table-driven test wires each plan to a specific verdict.
func newFakePlanAPI(t *testing.T, reps map[string]fakeResp) *httptest.Server {
	t.Helper()
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"fake","token_type":"bearer","expires_in":3600}`)
			return
		case "/plans":
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			rep, ok := reps[req.Name]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(rep.status)
			_, _ = io.WriteString(w, rep.body)
			return
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv
}

// withEnv sets each key for the duration of the test and restores the
// prior value on cleanup. Keeps the env-touching tests hermetic under
// -count=N and -race.
func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		prior, had := os.LookupEnv(k)
		if err := os.Setenv(k, v); err != nil {
			t.Fatalf("setenv %s: %v", k, err)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(k, prior)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
}

// projectFile is a small helper so a test can drive submit() without
// duplicating the `read → project` chain from run().
func projectFile(path string) (createPlanRequest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		return createPlanRequest{}, err
	}
	return project(data)
}
