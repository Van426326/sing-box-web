# kt-dat Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Daed GraphQL sync with a GitHub Contents API sync that commits generated `kt.txt` content to `Van426326/kt-dat`.

**Architecture:** Add `internal/ktdatsync` as the only sync implementation. Wire it into `main.go` and `internal/server` through a generic sync service interface, update the frontend to call `POST /api/ktdat/sync`, and update deployment docs/scripts to collect `KTDAT_*` variables instead of `DAED_*`.

**Tech Stack:** Go 1.22 standard library, GitHub Contents API, existing static HTML/CSS/JS, Bash install script.

## Global Constraints

- Sync target defaults to `KTDAT_REPO=Van426326/kt-dat`, `KTDAT_BRANCH=main`, `KTDAT_PATH=kt.txt`.
- `KTDAT_TOKEN` is required for syncing but optional at install time.
- `KTDAT_TOKEN` must never be exposed to the browser.
- Generated `kt.txt` is one CIDR per line from saved sing-box `route.rules[*].ip_cidr`.
- Keep the unsaved-change guard before syncing.
- Remove Daed GraphQL environment prompts, docs, and backend wiring.

---

### Task 1: kt-dat Sync Package

**Files:**
- Create: `internal/ktdatsync/sync.go`
- Create: `internal/ktdatsync/sync_test.go`

**Interfaces:**
- Consumes: `configmgr.LoadResult.Config`
- Produces: `func New(Config, ConfigLoader, *http.Client) *Service`
- Produces: `func (s *Service) Sync(ctx context.Context) (*Result, error)`
- Produces: `func ExtractIPCidrs(raw json.RawMessage) ([]string, error)`
- Produces: `func RenderKTText(cidrs []string) string`
- Produces errors: `ErrMissingConfig`, `ErrInvalidRepo`, `ErrGitHub`, `ErrConflict`

- [x] **Step 1: Write package tests**

Add tests for extraction, rendering, missing token, invalid repo, no-op, update, create, and conflict.

- [x] **Step 2: Run red tests**

Run: `GOCACHE=$PWD/.cache/go-build go test ./internal/ktdatsync`
Expected: FAIL because the package does not exist.

- [x] **Step 3: Implement package**

Implement GitHub Contents API calls with `Authorization: Bearer <token>`, `Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28`, base64 content handling, and result fields for target, changed, CIDR count, commit SHA, commit URL, and message.

- [x] **Step 4: Run green tests**

Run: `GOCACHE=$PWD/.cache/go-build go test ./internal/ktdatsync`
Expected: PASS.

### Task 2: Server and Main Wiring

**Files:**
- Modify: `main.go`
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`
- Delete or leave unused pending cleanup: `internal/daedsync/*`

**Interfaces:**
- Consumes: `ktdatsync.Service.Sync(ctx)`
- Produces: `POST /api/ktdat/sync`
- Produces server status mapping for kt-dat sync errors.

- [x] **Step 1: Update server tests**

Replace Daed sync tests with kt-dat sync tests covering success, missing config as 400, GitHub error as 502, and conflict as 409.

- [x] **Step 2: Run red server tests**

Run: `GOCACHE=$PWD/.cache/go-build go test ./internal/server`
Expected: FAIL because `/api/ktdat/sync` does not exist yet.

- [x] **Step 3: Update server and main**

Import `internal/ktdatsync`, construct it with `KTDAT_REPO`, `KTDAT_BRANCH`, `KTDAT_PATH`, `KTDAT_TOKEN`, and route `POST /api/ktdat/sync` to the sync service.

- [x] **Step 4: Remove Daed backend wiring**

Remove `internal/daedsync` from `main.go` and server tests. Delete `internal/daedsync` if no code imports it after kt-dat package owns CIDR extraction.

- [x] **Step 5: Run green server tests**

Run: `GOCACHE=$PWD/.cache/go-build go test ./internal/server`
Expected: PASS.

### Task 3: Frontend Sync UI

**Files:**
- Modify: `web/static/index.html`
- Modify: `web/static/app.js`

**Interfaces:**
- Consumes: `POST /api/ktdat/sync`
- Produces: button text "同步到 kt-dat"

- [x] **Step 1: Update frontend button and call**

Change the button id or handler to call `/api/ktdat/sync`, keep the unsaved-change guard, and show commit URL/count when returned.

- [x] **Step 2: Search for stale Daed direct-sync text**

Run: `rg -n "同步到 Daed|/api/daed|DAED_|daed graphql|dip\\(" web internal main.go README.md scripts`
Expected: no stale user-facing direct-sync references after docs task completes; frontend should be clean in this task.

### Task 4: Deployment Docs and Installer

**Files:**
- Modify: `README.md`
- Modify: `scripts/install.sh`
- Modify: `install_script_test.go`

**Interfaces:**
- Consumes: `KTDAT_REPO`, `KTDAT_BRANCH`, `KTDAT_PATH`, `KTDAT_TOKEN`
- Produces: installer env file entries and README deployment docs.

- [x] **Step 1: Update install script contract test**

Replace `DAED_GRAPHQL_URL` and `DAED_AUTHORIZATION` checks with `KTDAT_REPO`, `KTDAT_BRANCH`, `KTDAT_PATH`, `KTDAT_TOKEN`, and hidden token prompt.

- [x] **Step 2: Run red root test**

Run: `GOCACHE=$PWD/.cache/go-build go test .`
Expected: FAIL because install script and README still mention Daed env vars.

- [x] **Step 3: Update installer**

Prompt for kt-dat variables, write them to `/etc/kt-proxy/kt-proxy.env`, keep `KTDAT_TOKEN` hidden, and remove Daed env prompts.

- [x] **Step 4: Update README**

Document the kt-dat sync behavior, fine-grained PAT permissions, interactive and non-interactive install variables, and remove Daed GraphQL sync docs.

- [x] **Step 5: Verify installer syntax and root test**

Run: `bash -n scripts/install.sh`
Run: `GOCACHE=$PWD/.cache/go-build go test .`
Expected: both PASS.

### Task 5: Final Cleanup and Verification

**Files:**
- Delete: `internal/daedsync/sync.go`
- Delete: `internal/daedsync/sync_test.go`
- Modify: any stale docs/tests found by search.

**Interfaces:**
- Produces a codebase with no Daed GraphQL sync implementation.

- [x] **Step 1: Remove stale Daed sync package**

Delete `internal/daedsync` if nothing imports it.

- [x] **Step 2: Search stale references**

Run: `rg -n "DAED_|daedsync|/api/daed|同步到 Daed|Daed GraphQL|dip\\(|agh_session" . -g '!docs/superpowers/plans/2026-06-25-daed-route-sync.md' -g '!docs/superpowers/specs/2026-06-25-daed-route-sync-design.md' -g '!docs/superpowers/plans/2026-06-26-ktdat-sync.md'`
Expected: only historical docs may mention the old approach.

- [x] **Step 3: Run full verification**

Run: `bash -n scripts/install.sh`
Run: `GOCACHE=$PWD/.cache/go-build go test ./...`
Expected: both PASS.

- [x] **Step 4: Commit and push**

Commit message: `feat: sync route cidrs to kt-dat`
