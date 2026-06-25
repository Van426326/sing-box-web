# Daed Route Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "同步到 Daed" button that syncs sing-box `route.rules[*].ip_cidr` values into the Daed `# 家 & kt` / `dip(...) -> singbox` routing block.

**Architecture:** Add a focused `internal/daedsync` package for extraction, Daed GraphQL calls, and routing text replacement. Expose it through a new HTTP handler in `internal/server`, wire environment variables in `main.go`, and add a frontend button that calls the API and displays results in the existing alert area.

**Tech Stack:** Go 1.22+, standard library HTTP client, standard library tests, vanilla HTML/CSS/JavaScript.

## Global Constraints

- Use environment variables only: `DAED_GRAPHQL_URL` and `DAED_AUTHORIZATION`.
- `agh_session` cookie is not required and will not be supported in this version.
- If either environment variable is missing, the API returns a clear error and the page displays which variable is missing.
- `ip_cidr` may be a string or an array of strings.
- Empty values are ignored.
- Duplicate values are removed.
- Ordering keeps existing Daed IP order first, then appends missing sing-box IPs in route rule order.
- The sync only updates the first `dip(...) -> singbox` block after `# 家 & kt`.
- If `# 家 & kt` or the target `dip(...) -> singbox` block cannot be found, the sync fails without modifying Daed.
- The project directory is currently not a git repository; commit steps apply only if git has been initialized.

---

## File Structure

- Create `internal/daedsync/sync.go`: Daed sync service, GraphQL client, route IP extraction, routing block replacement.
- Create `internal/daedsync/sync_test.go`: unit tests for extraction, replacement, env errors, and GraphQL sequence.
- Modify `internal/server/server.go`: add optional Daed sync service interface and API endpoint.
- Modify `internal/server/server_test.go`: test Daed endpoint status mapping.
- Modify `main.go`: read `DAED_GRAPHQL_URL` and `DAED_AUTHORIZATION`, create Daed sync service.
- Modify `web/static/index.html`: add "同步到 Daed" button next to "检查并保存".
- Modify `web/static/app.js`: wire button to API and display result.
- Modify `README.md`: document Daed env vars.

---

### Task 1: Daed Sync Core

**Files:**
- Create: `internal/daedsync/sync.go`
- Create: `internal/daedsync/sync_test.go`

**Interfaces:**
- Produces: `type Service struct`
- Produces: `type Config struct { GraphQLURL string; Authorization string }`
- Produces: `func New(config Config, configLoader ConfigLoader, httpClient *http.Client) *Service`
- Produces: `func (s *Service) Sync(ctx context.Context) (*Result, error)`
- Produces: `type ConfigLoader interface { Load(ctx context.Context) (*configmgr.LoadResult, error) }`
- Produces: `type Result struct { RoutingID string; RoutingName string; Changed bool; ExistingCount int; SourceCount int; Added []string; Message string }`
- Produces: sentinel errors `ErrMissingConfig`, `ErrNoSelectedRouting`, `ErrMarkerNotFound`, `ErrTargetBlockNotFound`, `ErrGraphQL`
- Produces helpers: `ExtractIPCidrs(raw json.RawMessage) ([]string, error)`, `MergeRoutingBlock(routing string, source []string) (string, MergeResult, error)`

- [ ] **Step 1: Write failing tests**

Create `internal/daedsync/sync_test.go` with tests for:

```go
package daedsync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kt-proxy/internal/configmgr"
)

type fakeLoader struct {
	result *configmgr.LoadResult
	err    error
}

func (f fakeLoader) Load(ctx context.Context) (*configmgr.LoadResult, error) {
	return f.result, f.err
}

func TestExtractIPCidrsSupportsStringArrayAndDedupes(t *testing.T) {
	raw := json.RawMessage(`{"route":{"rules":[
		{"ip_cidr":"10.0.0.0/24"},
		{"ip_cidr":["10.0.0.0/24","192.168.1.1/32"]},
		{"domain":["example.com"]},
		{"ip_cidr":""}
	]}}`)

	got, err := ExtractIPCidrs(raw)
	if err != nil {
		t.Fatalf("ExtractIPCidrs returned error: %v", err)
	}
	want := []string{"10.0.0.0/24", "192.168.1.1/32"}
	assertStrings(t, got, want)
}

func TestMergeRoutingBlockAppendsMissingIPs(t *testing.T) {
	routing := "pname(mosdns) -> must_rules\n\n# 家 & kt\ndip(\n10.0.0.0/24,\n192.168.1.1/32\n) -> singbox\n\nfallback: proxy"
	source := []string{"192.168.1.1/32", "172.16.1.0/24"}

	updated, result, err := MergeRoutingBlock(routing, source)
	if err != nil {
		t.Fatalf("MergeRoutingBlock returned error: %v", err)
	}
	if !result.Changed {
		t.Fatal("Changed = false, want true")
	}
	assertStrings(t, result.Added, []string{"172.16.1.0/24"})
	if !strings.Contains(updated, "10.0.0.0/24,\n192.168.1.1/32,\n172.16.1.0/24") {
		t.Fatalf("updated routing missing merged block:\n%s", updated)
	}
}

func TestMergeRoutingBlockNoChange(t *testing.T) {
	routing := "# 家 & kt\ndip(\n10.0.0.0/24\n) -> singbox\n"
	updated, result, err := MergeRoutingBlock(routing, []string{"10.0.0.0/24"})
	if err != nil {
		t.Fatalf("MergeRoutingBlock returned error: %v", err)
	}
	if updated != routing {
		t.Fatalf("updated changed unexpectedly:\n%s", updated)
	}
	if result.Changed {
		t.Fatal("Changed = true, want false")
	}
}

func TestMergeRoutingBlockRequiresMarkerAndTargetBlock(t *testing.T) {
	if _, _, err := MergeRoutingBlock("dip(10.0.0.0/24) -> singbox", []string{"10.0.0.0/24"}); !errors.Is(err, ErrMarkerNotFound) {
		t.Fatalf("missing marker err = %v", err)
	}
	if _, _, err := MergeRoutingBlock("# 家 & kt\ndomain(example.com) -> direct", []string{"10.0.0.0/24"}); !errors.Is(err, ErrTargetBlockNotFound) {
		t.Fatalf("missing block err = %v", err)
	}
}

func TestSyncRequiresEnvironmentConfig(t *testing.T) {
	service := New(Config{}, fakeLoader{}, http.DefaultClient)
	_, err := service.Sync(context.Background())
	if !errors.Is(err, ErrMissingConfig) {
		t.Fatalf("Sync error = %v, want ErrMissingConfig", err)
	}
}

func TestSyncQueriesUpdatesAndRunsDaed(t *testing.T) {
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		var payload graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, payload.OperationName)
		switch payload.OperationName {
		case "Routings":
			writeTestJSON(t, w, map[string]any{"data": map[string]any{"routings": []any{
				map[string]any{"id": "r1", "name": "default", "selected": true, "routing": map[string]any{"string": "# 家 & kt\ndip(\n10.0.0.0/24\n) -> singbox\n"}},
			}}})
		case "UpdateRouting":
			variables := payload.Variables
			if variables["id"] != "r1" {
				t.Fatalf("update id = %v", variables["id"])
			}
			if !strings.Contains(variables["routing"].(string), "192.168.1.1/32") {
				t.Fatalf("updated routing missing new IP: %s", variables["routing"])
			}
			writeTestJSON(t, w, map[string]any{"data": map[string]any{"updateRouting": map[string]any{"id": "r1"}}})
		case "Run":
			writeTestJSON(t, w, map[string]any{"data": map[string]any{"run": true}})
		default:
			t.Fatalf("unexpected operation: %s", payload.OperationName)
		}
	}))
	defer server.Close()

	loader := fakeLoader{result: &configmgr.LoadResult{Config: json.RawMessage(`{"route":{"rules":[{"ip_cidr":"192.168.1.1/32"}]}}`), LoadedAt: time.Now()}}
	service := New(Config{GraphQLURL: server.URL, Authorization: "Bearer token"}, loader, server.Client())

	result, err := service.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if !result.Changed || result.RoutingID != "r1" || result.RoutingName != "default" {
		t.Fatalf("result mismatch: %+v", result)
	}
	assertStrings(t, requests, []string{"Routings", "UpdateRouting", "Run"})
}

func assertStrings(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q; got=%v", i, got[i], want[i], got)
		}
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/daedsync`

Expected: FAIL because package functions and types are undefined.

- [ ] **Step 3: Implement `internal/daedsync/sync.go`**

Implement the types and logic described by the tests:

- Parse sing-box config as generic JSON.
- Extract `route.rules[*].ip_cidr` strings and arrays.
- Locate `# 家 & kt`, then scan from that point for the next `dip(`.
- Find the matching closing `)` and require the following text before line end to contain `-> singbox`.
- Parse existing entries inside `dip(...)` by commas and whitespace.
- Replace only that block with:

```text
dip(
existing1,
existing2,
new1
) -> singbox
```

- Send GraphQL JSON requests with `Authorization` and `Content-Type: application/json`.
- Treat HTTP non-2xx, GraphQL `errors`, and malformed responses as `ErrGraphQL`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/daedsync`

Expected: PASS.

---

### Task 2: Server API Wiring

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`
- Modify: `main.go`

**Interfaces:**
- Consumes: `daedsync.Service.Sync(ctx)`
- Produces: `POST /api/daed/sync-route-rules`
- Produces: `func New(m ConfigService, staticFS fs.FS, daed DaedSyncService) http.Handler`

- [ ] **Step 1: Add failing server tests**

Add tests that:

- `POST /api/daed/sync-route-rules` returns 200 and the sync result.
- `daedsync.ErrMissingConfig` maps to HTTP 400.
- `daedsync.ErrMarkerNotFound` maps to HTTP 422.
- Nil Daed service maps to HTTP 400 with missing config style message.

- [ ] **Step 2: Run server tests to verify they fail**

Run: `go test ./internal/server`

Expected: FAIL because handler signature and endpoint do not exist.

- [ ] **Step 3: Implement API wiring**

Modify `server.New` to accept a third parameter:

```go
type DaedSyncService interface {
	Sync(ctx context.Context) (*daedsync.Result, error)
}
```

Add `POST /api/daed/sync-route-rules`.

Status mapping:

- `ErrMissingConfig` -> 400
- `ErrNoSelectedRouting`, `ErrMarkerNotFound`, `ErrTargetBlockNotFound` -> 422
- `ErrGraphQL` -> 502
- other errors -> 500

Update existing tests and `main.go` to pass the new dependency.

- [ ] **Step 4: Run all Go tests**

Run: `go test ./...`

Expected: PASS.

---

### Task 3: Frontend Button and Documentation

**Files:**
- Modify: `web/static/index.html`
- Modify: `web/static/app.js`
- Modify: `README.md`

**Interfaces:**
- Consumes: `POST /api/daed/sync-route-rules`
- Produces: visible "同步到 Daed" button beside "检查并保存".

- [ ] **Step 1: Add button**

In `web/static/index.html`, add:

```html
<button id="syncDaedBtn" type="button">同步到 Daed</button>
```

next to the save button.

- [ ] **Step 2: Wire frontend action**

In `web/static/app.js`:

- Add click listener for `syncDaedBtn`.
- Implement `syncDaed()` that POSTs to `/api/daed/sync-route-rules`.
- On success, display `body.message`, routing name, and added IPs if any.
- On error, display `body.error`.

- [ ] **Step 3: Document environment variables**

In `README.md`, add:

```markdown
## Daed Sync

Set these variables to enable the "同步到 Daed" button:

- `DAED_GRAPHQL_URL`
- `DAED_AUTHORIZATION`

The sync reads sing-box `route.rules[*].ip_cidr` and updates the selected Daed routing block marked by `# 家 & kt` and `dip(...) -> singbox`.
```

- [ ] **Step 4: Run tests and build**

Run:

```bash
go test ./...
go build -o kt-proxy .
```

Expected: both PASS.

- [ ] **Step 5: Manual UI verification**

Run the local server without Daed env vars:

```bash
SING_BOX_CONFIG_PATH=./sing-box-config-example.json SING_BOX_EXAMPLE_PATH=./sing-box-config-example.json SING_BOX_BIN=/bin/true SYSTEMCTL_BIN=/bin/true KT_PROXY_ADDR=:8090 ./kt-proxy
```

Open `http://localhost:8090`, click "同步到 Daed", and verify the page shows missing `DAED_GRAPHQL_URL` / `DAED_AUTHORIZATION`.
