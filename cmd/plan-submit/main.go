// Command plan-submit is the catalog's auto-release leg for normal Plans (the
// Phase-3 sibling of sync-templates-to-controller, which handles PlanTemplates).
//
// On merge to main the release postsubmit runs this on BOTH clusters; each
// invocation submits every plans/*.yaml (excluding example-*) to its OWN local
// plan-api via POST /plans. plan-api is the SINGLE writer — it validates, applies
// the auto-composition injection policy, and creates the Plan CRD (always paused,
// i.e. a proposal a human unpauses). This tool is a THIN forwarder: it does not
// re-validate or re-inject — the exact same contract the plan MCP's create_plan
// forwards onto, just with an s2s token instead of a forwarded user bearer, and
// reading YAML files instead of structured tool args.
//
// "Both clusters" needs no cross-cluster networking or primary-gating: the
// postsubmit already fires on both clusters, so each self-submits to its local
// plan-api and both estates receive the paused proposal.
//
// Auth: OAuth2 client_credentials (LEARTECH_PLAN_SUBMIT_CLIENT_ID/_SECRET against
// LEARTECH_AUTH_TOKEN_URL) minting a token carrying the internal_services scope —
// one of the three plan-api accepts for POST /plans (mcp:write | internal_services
// | PlatformAdmin). Idempotent: an already-submitted plan (HTTP 409) is a success.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	dir := flag.String("dir", "plans", "directory of Plan YAML files to submit")
	planAPI := flag.String("plan-api-url", os.Getenv("LEARTECH_PLAN_API_URL"), "base URL of the local cluster's plan-api (e.g. http://leartech-plan-api.jx-staging:8080)")
	dryRun := flag.Bool("dry-run", false, "project + print the CreatePlanRequest payloads without calling plan-api (no token needed)")
	flag.Parse()

	if err := run(*dir, *planAPI, *dryRun); err != nil {
		fmt.Fprintln(os.Stderr, "plan-submit:", err)
		os.Exit(1)
	}
}

func run(dir, planAPI string, dryRun bool) error {
	files, err := discover(dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Printf("plan-submit: no submittable Plan YAML under %s/ (excluding example-*) — nothing to do.\n", dir)
		return nil
	}

	// Project every file up front so a malformed plan fails the run BEFORE we
	// mint a token or mutate any cluster state.
	type job struct {
		file string
		req  createPlanRequest
	}
	var jobs []job
	for _, f := range files {
		data, rerr := os.ReadFile(f) //nolint:gosec // release-controlled repo paths from discover()
		if rerr != nil {
			return fmt.Errorf("read %s: %w", f, rerr)
		}
		req, perr := project(data)
		if perr != nil {
			return fmt.Errorf("project %s: %w", f, perr)
		}
		jobs = append(jobs, job{file: f, req: req})
	}

	if dryRun {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		for _, j := range jobs {
			fmt.Printf("# %s -> POST %s/plans\n", j.file, strings.TrimRight(planAPI, "/"))
			_ = enc.Encode(j.req)
		}
		fmt.Printf("plan-submit: DRY-RUN — projected %d plan(s), no calls made.\n", len(jobs))
		return nil
	}

	if planAPI == "" {
		return errors.New("plan-api-url is required (flag -plan-api-url or LEARTECH_PLAN_API_URL)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token, err := fetchToken(ctx)
	if err != nil {
		return fmt.Errorf("mint s2s token: %w", err)
	}

	var failed int
	for _, j := range jobs {
		status, body, serr := submit(ctx, planAPI, token, j.req)
		switch {
		case serr != nil:
			fmt.Printf("  FAIL %s (%s): %v\n", j.req.Name, j.file, serr)
			failed++
		case status >= 200 && status < 300:
			fmt.Printf("  OK   %s (%s) -> HTTP %d\n", j.req.Name, j.file, status)
		case status == http.StatusConflict:
			// Already submitted — the proposal exists. Idempotent success.
			fmt.Printf("  SKIP %s (%s) -> HTTP 409 already exists\n", j.req.Name, j.file)
		default:
			fmt.Printf("  FAIL %s (%s) -> HTTP %d: %s\n", j.req.Name, j.file, status, truncate(body, 300))
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d plan(s) failed to submit", failed, len(jobs))
	}
	fmt.Printf("plan-submit: submitted %d plan(s) to %s\n", len(jobs), planAPI)
	return nil
}

// discover returns the sorted set of submittable plan files under dir: *.yaml
// excluding example-*.yaml (documentation, not runtime proposals — mirrors the
// example skip in sync-templates-to-controller.sh).
func discover(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		if strings.HasPrefix(name, "example-") || strings.HasSuffix(name, "-example.yaml") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	sort.Strings(out)
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// fetchToken mints an s2s access token via OAuth2 client_credentials. The minted
// token must carry the internal_services scope (plan-api's canWrite accepts
// internal_services | leartechapi:mcp:write | PlatformAdmin). audience is passed
// so Hydra stamps aud=plan-api (plan-api validates the audience).
func fetchToken(ctx context.Context) (string, error) {
	tokenURL := os.Getenv("LEARTECH_AUTH_TOKEN_URL")
	clientID := os.Getenv("LEARTECH_PLAN_SUBMIT_CLIENT_ID")
	clientSecret := os.Getenv("LEARTECH_PLAN_SUBMIT_CLIENT_SECRET")
	// Estate conventions (see leartech-auth-service setup-internal-clients.sh +
	// the controller's LEARTECH_AUTH_SCOPE): the s2s scope is the dotted
	// leartechapi.internal_services, and plan-api's audience is the stable
	// logical name leartech-plan-api (LEARTECH_AUTH_AUDIENCE).
	scope := envOr("LEARTECH_PLAN_SUBMIT_SCOPE", "leartechapi.internal_services")
	audience := envOr("LEARTECH_PLAN_SUBMIT_AUDIENCE", "leartech-plan-api")
	if tokenURL == "" || clientID == "" || clientSecret == "" {
		return "", errors.New("LEARTECH_AUTH_TOKEN_URL, LEARTECH_PLAN_SUBMIT_CLIENT_ID and LEARTECH_PLAN_SUBMIT_CLIENT_SECRET must be set")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("scope", scope)
	if audience != "" {
		form.Set("audience", audience)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token endpoint HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if tok.AccessToken == "" {
		return "", errors.New("token endpoint returned an empty access_token")
	}
	return tok.AccessToken, nil
}

// submit POSTs one CreatePlanRequest to plan-api's /plans and returns the HTTP
// status + response body.
func submit(ctx context.Context, planAPI, token string, req createPlanRequest) (int, string, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return 0, "", err
	}
	endpoint := strings.TrimRight(planAPI, "/") + "/plans"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(body), nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
