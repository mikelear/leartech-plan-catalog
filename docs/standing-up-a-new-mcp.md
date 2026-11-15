# Standing up a new MCP-fronted service

Read this BEFORE you create a new backing API + MCP forwarder in this estate (the
`leartech-mcp-servers` pattern: `plan`, `design`, `third_party`,
`platform_capabilities`, `gateway_mgmt`). It is the order of work and the auth
model to follow so you do NOT rediscover the audience/token-exchange reality the
hard way — which costs a day of 401s in a shell at the end instead of a checklist
at the start.

The shape every one of these follows: a thin **MCP forwarder** (in
leartech-mcp-servers) over a **backing resource-server API** (its own repo),
deployed via an **umbrella** chart, registered for **Lighthouse** and in
**Hydra/auth-service**. The MCP does no business logic and proxies no bulk data;
it forwards the caller's Bearer to the API, and the API's own gate is
authoritative.

## The one fact that governs the whole auth design

**This estate's Ory Hydra is v25 OSS. It has NO RFC 8693 token-exchange, and it
never will.** `LEARTECH_TOKEN_EXCHANGE_ENABLED` exists in the forwarder code but
is OFF everywhere and **is not a feature flag you can turn on** — the capability
is absent from the deployed identity provider. proven-by the tokenexchange
package's noop default (an empty env yields `Disabled()==true`) and stated in
auth-service `internal/config/config.go`, `internal/handlers/extra_audience.go`,
and the infra-umbrella overlay.

Consequence: **a caller's token cannot be re-audienced between the MCP host and
the backing service.** A DCR/shell user token is audienced to the **MCP host
URL**, not to your service. So:

- **You do NOT design around token-exchange.** Delete that idea before you start.
- Instead, the backing service **additively accepts the MCP-host audience**, with
  the permission/scope check as the real authority.

Get this wrong and everything passes unit tests, deploys green, and then 401s the
first time the shell calls a tool — because the inbound hop (shell → service) was
never the hop you tested.

## Classify your callers FIRST (both usually apply)

| caller | token audience | how the service accepts it |
|---|---|---|
| **user via the public MCP** (DCR / `ship-proven mcp add`) | the **MCP host URL** | **MultiAudience**: the service accepts the host audience additively (see reference below). PermAdmin/scope is the authority. |
| **s2s** (service → service, callerless) | minted for the **target service** | the target must be on the minting client's allow-list (the `mcp-internal-services` token-exchange table in auth-service — note this uses Hydra's client `audience` allow-list, NOT 8693). |

Most MCP-fronted services have BOTH. gateway_mgmt has both: a PlatformAdmin
driving it from the shell (user path) and the service forwarding to the ai-gateway
(s2s fallback path).

## The reference implementations — copy these, not a cousin

- **Inbound user-token acceptance (the hop that bites):**
  `leartech-auth-service/internal/handlers/extra_audience.go`
  (`MultiAudienceMiddleware`) + `internal/config/config.go`
  (`ExtraAcceptedAudiences`, env `LEARTECH_AUTH_EXTRA_ACCEPTED_AUDIENCES`), proven-by
  `TestMultiAudience`. The overlay sets the extra audience to the public MCP host
  (leartech-infra-umbrella values `extraAcceptedAudiences`).
- **The forwarder + fail-closed authz gate:** any of
  `leartech-mcp-servers/internal/{plan,third_party,platform_capabilities}server`
  — the `ClientFactory` + `do()` + `authorize*` shape.
- **The backing resource-server skeleton:** `leartech-plan-api` (DB-less) or
  `leartech-artifact-api` (with a datastore) — Verifier-based `BearerAuth`
  (NO `AUTH_ENABLED` toggle — auth-conformance forbids it), `.authprofile`,
  chart, `rfc_conformance_test.go`.

A frequent trap: cloning the **frontend/backend `audiences` + `userTokenDefaultAudiences`**
lists (the MINT side) and thinking that is the whole job. Those govern what Hydra
will stamp into a token. They do NOT make a resource server ACCEPT the host
audience — that is MultiAudience, a separate mechanism on the service.

## The order of work

1. **Read the constraint, not the config.** Grep `8693`, `token.?exchange`,
   `OSS Hydra`. Confirm re-audiencing does not exist here before designing auth.
2. **Scaffold the backing API** from the golden resource-server. Set
   `.authprofile` (`inbound-resource-server`, or `dual-role` if it also mints s2s).
   Verifier-based auth, no disable toggle.
3. **Its own strict scope family** in the service's `internal/authz` AND mirrored
   in the MCP forwarder's gate AND in the auth-service DCR `allowedScopes` ceiling
   (ceiling, not floor). The three must agree; pin with a test on each side.
4. **Inbound audience acceptance.** Add MultiAudience + ExtraAcceptedAudiences to
   the service, and an `rfc_conformance_test.go` case: *a token aud=MCP-host is
   accepted on the gated routes; wrong-issuer / unknown-aud is refused.* This is
   the test that would have caught the gateway_mgmt 401.
5. **The MCP forwarder** in leartech-mcp-servers: self-register (internal +
   public as appropriate), `GatedScopes()` = the family, fail-closed authz,
   dormant-until-wired on an empty backend URL. Blank-import it in
   `cmd/server/main.go`. Add the backend to `lokiserver` `frontedServices`.
6. **Register everywhere a new service must be known** (the "past pain" list):
   - jx gitops `source-config.yaml` — Lighthouse watches the repo.
   - GitHub webhook on the new repo (jx + az hook URLs, cluster HMAC secret) —
     a brand-new repo has none, and without it Lighthouse never sees the PR.
   - Repo collaborators: the clone identity (LeartechKeeperBot) + the bots, else
     the pipeline can't clone a private repo.
   - auth-service DCR `allowedScopes` (the family) + frontend/backend audiences +
     `userTokenDefaultAudiences` (both sides — the intersection rule) + the
     `mcp-internal-services` audience list if it is an s2s target + the authgraph
     edge.
   - the umbrella: dependency + `enabled` toggle + per-subchart auth
     (`auth.serverUrl` → `LEARTECH_AUTH_ISSUER`, NOT `auth.issuer`) + the MCP's
     `<svc>ApiUrl`/`<svc>ApiAudience` + **`extraAcceptedAudiences` = public host**.
   - Renovate `repositories` list (the verify gate fails a deployed repo that is
     absent from it).
   - seed base tags `v0.0.0-{gcp,az}` so `next-version` computes a clean `0.0.1`
     (a bare repo tags a `…-SNAPSHOT` and helm-release can't find the tgz).
   - never commit `.jx/variables.sh` (gitignore it) — a committed PR-build copy
     shadows the release-time VERSION and breaks the first release.
7. **Prove inbound BEFORE wiring downstream.** Deploy the service NOT yet wired to
   its real backend, and prove shell → service auth works (accepted or
   deliberately refused). ONLY then wire service → backend. We did this
   deliberately for gateway_mgmt and then proved the wrong hop; do the inbound
   one.
8. **Test with low-risk fixtures, never real third-party keys.** Keyless/mock
   backends (ai-gateway's `echo`/`mock`, a wire-mock) exercise the whole path at
   zero spend and zero secret. See the gateway-mgmt secrets+testing note.

## Secrets never flow through an MCP

If your service touches credentials, the value goes into the backend
(GSM/AKV) via a **printed-not-executed `scripts/upload-secrets-*.sh`** on the
owner's machine, and the chart reads it by **reference** (ExternalSecret). The MCP
carries a reference at most, never a secret value — the same R3 shape as
third_party `promote_finding` (one injectable `gh` runner, tested with a recorder).

## The checklist, condensed

- [ ] confirmed no 8693; designed for host-audience acceptance, not exchange
- [ ] backing API: Verifier auth, no AUTH_ENABLED, `.authprofile` set
- [ ] strict scope family in service authz + MCP gate + DCR ceiling (3 agree, tested)
- [ ] MultiAudience + ExtraAcceptedAudiences + conformance test (host-aud accepted)
- [ ] MCP forwarder: GatedScopes, fail-closed, dormant-until-wired, blank import, frontedServices
- [ ] source-config + webhook + collaborators + Renovate + base tags + gitignore .jx/variables.sh
- [ ] umbrella: dep + toggle + auth.serverUrl + ApiUrl/Audience + extraAcceptedAudiences
- [ ] proved shell→service auth BEFORE wiring service→backend
- [ ] integration against keyless/mock fixtures; no real third-party keys
- [ ] no secret value through the MCP; upload-script + ExternalSecret reference only
