# sing-box Web Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an unauthenticated Ubuntu web manager for viewing, editing, checking, saving, and reloading `/etc/sing-box/config.json`.

**Architecture:** A Go HTTP server embeds static frontend assets and exposes JSON APIs. Backend config-management code owns file loading, pretty JSON, backups, `sing-box check`, and `systemctl reload sing-box`; the browser owns interactive editing of one canonical config object.

**Tech Stack:** Go 1.22+, standard library HTTP server, `embed.FS`, vanilla HTML/CSS/JavaScript, Go `testing` package.

## Global Constraints

- Listen address defaults to `:8090`.
- Config path defaults to `/etc/sing-box/config.json`.
- Example fallback path defaults to `sing-box-config-example.json`.
- Environment overrides: `KT_PROXY_ADDR`, `SING_BOX_CONFIG_PATH`, `SING_BOX_EXAMPLE_PATH`, `SING_BOX_BIN`, `SYSTEMCTL_BIN`.
- First version has no login, TLS, users, permissions, or CSRF protection.
- Saving always targets the configured config path, never the example fallback path.
- `sing-box check -c <temporary-file>` must pass before overwriting the real config file.
- `systemctl reload sing-box` runs only after the checked config has been written.
- Unknown sing-box fields must be preserved.
- The project directory is currently not a git repository; commit steps apply only if git has been initialized.

---

## File Structure

- Create `go.mod`: module metadata.
- Create `main.go`: entry point, environment configuration, HTTP server startup.
- Create `internal/configmgr/manager.go`: config load/save orchestration, command execution interfaces, metadata types.
- Create `internal/configmgr/manager_test.go`: backend tests for load fallback and save behavior.
- Create `internal/server/server.go`: HTTP handlers and embedded static file serving.
- Create `internal/server/server_test.go`: API handler tests.
- Create `web/static/index.html`: application shell.
- Create `web/static/styles.css`: responsive application styling.
- Create `web/static/app.js`: browser state management, tables, modals, JSON editor, save flow.
- Create `README.md`: build, run, environment variables, systemd deployment notes.

---

### Task 1: Go Module and Config Manager Core

**Files:**
- Create: `go.mod`
- Create: `internal/configmgr/manager.go`
- Create: `internal/configmgr/manager_test.go`

**Interfaces:**
- Produces: `type Paths struct { ConfigPath string; ExamplePath string }`
- Produces: `type Commands struct { SingBox string; Systemctl string }`
- Produces: `type Manager struct`
- Produces: `func New(paths Paths, commands Commands, runner Runner) *Manager`
- Produces: `func (m *Manager) Load(ctx context.Context) (*LoadResult, error)`
- Produces: `func (m *Manager) Save(ctx context.Context, raw json.RawMessage) (*SaveResult, error)`
- Produces: `type Runner interface { Run(ctx context.Context, name string, args ...string) CommandResult }`
- Produces: `type CommandResult struct { Stdout string; Stderr string; Err error }`
- Produces: sentinel errors `ErrInvalidJSON`, `ErrCheckFailed`, `ErrReloadFailed`

- [ ] **Step 1: Create `go.mod`**

```go
module kt-proxy

go 1.22
```

- [ ] **Step 2: Write failing config manager tests**

Create `internal/configmgr/manager_test.go`:

```go
package configmgr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	results []CommandResult
	calls   []fakeCall
}

type fakeCall struct {
	name string
	args []string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	f.calls = append(f.calls, fakeCall{name: name, args: append([]string(nil), args...)})
	if len(f.results) == 0 {
		return CommandResult{}
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result
}

func TestLoadReadsPrimaryConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	examplePath := filepath.Join(dir, "example.json")
	mustWrite(t, configPath, `{"outbounds":[{"tag":"direct"}],"route":{"rules":[{"action":"sniff"}],"final":"direct"}}`)
	mustWrite(t, examplePath, `{"outbounds":[],"route":{"rules":[]}}`)

	manager := New(Paths{ConfigPath: configPath, ExamplePath: examplePath}, Commands{}, &fakeRunner{})
	result, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if result.Source != configPath {
		t.Fatalf("Source = %q, want %q", result.Source, configPath)
	}
	if result.Fallback {
		t.Fatal("Fallback = true, want false")
	}
	if result.OutboundCount != 1 || result.RouteRuleCount != 1 || result.RouteFinal != "direct" {
		t.Fatalf("metadata mismatch: %+v", result)
	}
}

func TestLoadFallsBackToExampleConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "missing.json")
	examplePath := filepath.Join(dir, "example.json")
	mustWrite(t, examplePath, `{"outbounds":[{"tag":"sample"}],"route":{"rules":[],"final":"sample"}}`)

	manager := New(Paths{ConfigPath: configPath, ExamplePath: examplePath}, Commands{}, &fakeRunner{})
	result, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if result.Source != examplePath {
		t.Fatalf("Source = %q, want %q", result.Source, examplePath)
	}
	if !result.Fallback {
		t.Fatal("Fallback = false, want true")
	}
}

func TestSaveRejectsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	mustWrite(t, configPath, `{"old":true}`)
	runner := &fakeRunner{}
	manager := New(Paths{ConfigPath: configPath}, Commands{SingBox: "sing-box", Systemctl: "systemctl"}, runner)

	_, err := manager.Save(context.Background(), json.RawMessage(`{"broken":`))
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("Save error = %v, want ErrInvalidJSON", err)
	}
	content := mustRead(t, configPath)
	if content != `{"old":true}` {
		t.Fatalf("config changed to %q", content)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestSaveDoesNotOverwriteWhenCheckFails(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	mustWrite(t, configPath, `{"old":true}`)
	runner := &fakeRunner{results: []CommandResult{{Stderr: "bad config", Err: errors.New("exit status 1")}}}
	manager := New(Paths{ConfigPath: configPath}, Commands{SingBox: "sing-box", Systemctl: "systemctl"}, runner)

	_, err := manager.Save(context.Background(), json.RawMessage(`{"new":true}`))
	if !errors.Is(err, ErrCheckFailed) {
		t.Fatalf("Save error = %v, want ErrCheckFailed", err)
	}
	if content := mustRead(t, configPath); content != `{"old":true}` {
		t.Fatalf("config changed to %q", content)
	}
	if len(runner.calls) != 1 || runner.calls[0].name != "sing-box" {
		t.Fatalf("calls = %+v, want one sing-box check", runner.calls)
	}
}

func TestSaveBacksUpWritesAndReloads(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	mustWrite(t, configPath, `{"old":true}`)
	runner := &fakeRunner{results: []CommandResult{{Stdout: "check ok"}, {Stdout: "reload ok"}}}
	manager := New(Paths{ConfigPath: configPath}, Commands{SingBox: "sing-box", Systemctl: "systemctl"}, runner)

	result, err := manager.Save(context.Background(), json.RawMessage(`{"new":true}`))
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %+v, want check and reload", runner.calls)
	}
	if runner.calls[0].name != "sing-box" || strings.Join(runner.calls[0].args[:2], " ") != "check -c" {
		t.Fatalf("check call = %+v", runner.calls[0])
	}
	if runner.calls[1].name != "systemctl" || strings.Join(runner.calls[1].args, " ") != "reload sing-box" {
		t.Fatalf("reload call = %+v", runner.calls[1])
	}
	if result.BackupPath == "" {
		t.Fatal("BackupPath is empty")
	}
	if backup := mustRead(t, result.BackupPath); backup != `{"old":true}` {
		t.Fatalf("backup = %q, want old config", backup)
	}
	if content := mustRead(t, configPath); strings.TrimSpace(content) != `{"new": true}` {
		t.Fatalf("config = %q, want pretty new config", content)
	}
}

func TestSaveReportsReloadFailureWithBackupPath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	mustWrite(t, configPath, `{"old":true}`)
	runner := &fakeRunner{results: []CommandResult{{Stdout: "check ok"}, {Stderr: "reload failed", Err: errors.New("exit status 1")}}}
	manager := New(Paths{ConfigPath: configPath}, Commands{SingBox: "sing-box", Systemctl: "systemctl"}, runner)

	result, err := manager.Save(context.Background(), json.RawMessage(`{"new":true}`))
	if !errors.Is(err, ErrReloadFailed) {
		t.Fatalf("Save error = %v, want ErrReloadFailed", err)
	}
	if result == nil || result.BackupPath == "" {
		t.Fatalf("result = %+v, want backup path", result)
	}
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/configmgr`

Expected: FAIL because `New`, types, and errors are undefined.

- [ ] **Step 4: Implement config manager**

Create `internal/configmgr/manager.go`:

```go
package configmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var (
	ErrInvalidJSON  = errors.New("invalid json")
	ErrCheckFailed  = errors.New("sing-box check failed")
	ErrReloadFailed = errors.New("sing-box reload failed")
)

type Paths struct {
	ConfigPath  string
	ExamplePath string
}

type Commands struct {
	SingBox   string
	Systemctl string
}

type Runner interface {
	Run(ctx context.Context, name string, args ...string) CommandResult
}

type CommandResult struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Err    error  `json:"-"`
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
}

type Manager struct {
	paths    Paths
	commands Commands
	runner   Runner
	now      func() time.Time
}

type LoadResult struct {
	Config         json.RawMessage `json:"config"`
	ConfigPath     string          `json:"configPath"`
	Source         string          `json:"source"`
	Fallback       bool            `json:"fallback"`
	LoadError      string          `json:"loadError,omitempty"`
	LoadedAt       time.Time       `json:"loadedAt"`
	OutboundCount  int             `json:"outboundCount"`
	RouteRuleCount int             `json:"routeRuleCount"`
	RouteFinal     string          `json:"routeFinal"`
}

type SaveResult struct {
	BackupPath string        `json:"backupPath,omitempty"`
	Check      CommandResult `json:"check"`
	Reload     CommandResult `json:"reload"`
}

func New(paths Paths, commands Commands, runner Runner) *Manager {
	if paths.ConfigPath == "" {
		paths.ConfigPath = "/etc/sing-box/config.json"
	}
	if paths.ExamplePath == "" {
		paths.ExamplePath = "sing-box-config-example.json"
	}
	if commands.SingBox == "" {
		commands.SingBox = "sing-box"
	}
	if commands.Systemctl == "" {
		commands.Systemctl = "systemctl"
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Manager{
		paths:    paths,
		commands: commands,
		runner:   runner,
		now:      time.Now,
	}
}

func (m *Manager) Load(ctx context.Context) (*LoadResult, error) {
	primary, err := os.ReadFile(m.paths.ConfigPath)
	source := m.paths.ConfigPath
	fallback := false
	loadErr := ""
	raw := primary
	if err != nil {
		loadErr = err.Error()
		example, exampleErr := os.ReadFile(m.paths.ExamplePath)
		if exampleErr != nil {
			return nil, fmt.Errorf("read config %s failed: %w; read example %s failed: %v", m.paths.ConfigPath, err, m.paths.ExamplePath, exampleErr)
		}
		raw = example
		source = m.paths.ExamplePath
		fallback = true
	}

	pretty, metadata, err := normalize(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	return &LoadResult{
		Config:         pretty,
		ConfigPath:     m.paths.ConfigPath,
		Source:         source,
		Fallback:       fallback,
		LoadError:      loadErr,
		LoadedAt:       m.now(),
		OutboundCount:  metadata.OutboundCount,
		RouteRuleCount: metadata.RouteRuleCount,
		RouteFinal:     metadata.RouteFinal,
	}, nil
}

func (m *Manager) Save(ctx context.Context, raw json.RawMessage) (*SaveResult, error) {
	pretty, _, err := normalize(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}

	tempFile, err := os.CreateTemp("", "kt-proxy-sing-box-config-*.json")
	if err != nil {
		return nil, err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if _, err := tempFile.Write(pretty); err != nil {
		tempFile.Close()
		return nil, err
	}
	if err := tempFile.Close(); err != nil {
		return nil, err
	}

	result := &SaveResult{}
	result.Check = m.runner.Run(ctx, m.commands.SingBox, "check", "-c", tempPath)
	if result.Check.Err != nil {
		return result, fmt.Errorf("%w: %v", ErrCheckFailed, result.Check.Err)
	}

	backupPath, err := m.backupConfig()
	if err != nil {
		return result, err
	}
	result.BackupPath = backupPath

	if err := os.WriteFile(m.paths.ConfigPath, pretty, 0o600); err != nil {
		return result, err
	}

	result.Reload = m.runner.Run(ctx, m.commands.Systemctl, "reload", "sing-box")
	if result.Reload.Err != nil {
		return result, fmt.Errorf("%w: %v", ErrReloadFailed, result.Reload.Err)
	}
	return result, nil
}

func (m *Manager) backupConfig() (string, error) {
	content, err := os.ReadFile(m.paths.ConfigPath)
	if err != nil {
		return "", err
	}
	backupPath := fmt.Sprintf("%s.bak.%s", m.paths.ConfigPath, m.now().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(backupPath, content, 0o600); err != nil {
		return "", err
	}
	return backupPath, nil
}

type metadata struct {
	OutboundCount  int
	RouteRuleCount int
	RouteFinal     string
}

func normalize(raw []byte) (json.RawMessage, metadata, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, metadata{}, err
	}
	pretty, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, metadata{}, err
	}
	pretty = append(pretty, '\n')
	return pretty, collectMetadata(value), nil
}

func collectMetadata(value any) metadata {
	root, _ := value.(map[string]any)
	var md metadata
	if outbounds, ok := root["outbounds"].([]any); ok {
		md.OutboundCount = len(outbounds)
	}
	if route, ok := root["route"].(map[string]any); ok {
		if rules, ok := route["rules"].([]any); ok {
			md.RouteRuleCount = len(rules)
		}
		if final, ok := route["final"].(string); ok {
			md.RouteFinal = final
		}
	}
	return md
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/configmgr`

Expected: PASS.

- [ ] **Step 6: Commit if git exists**

Run only if `git rev-parse --is-inside-work-tree` succeeds:

```bash
git add go.mod internal/configmgr/manager.go internal/configmgr/manager_test.go
git commit -m "feat: add sing-box config manager"
```

---

### Task 2: HTTP API and Static Server

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/server_test.go`
- Create: `main.go`
- Create: `web/static/index.html`

**Interfaces:**
- Consumes: `configmgr.Manager.Load(ctx)` and `configmgr.Manager.Save(ctx, raw)`
- Produces: `func New(m *configmgr.Manager, staticFS fs.FS) http.Handler`
- Produces: `GET /api/config`
- Produces: `POST /api/config/save`
- Produces: `GET /` static app shell

- [ ] **Step 1: Write failing server tests**

Create `internal/server/server_test.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"kt-proxy/internal/configmgr"
)

type fakeConfigService struct {
	loadResult *configmgr.LoadResult
	loadErr    error
	saveResult *configmgr.SaveResult
	saveErr    error
	saveBody   json.RawMessage
}

func (f *fakeConfigService) Load(ctx context.Context) (*configmgr.LoadResult, error) {
	return f.loadResult, f.loadErr
}

func (f *fakeConfigService) Save(ctx context.Context, raw json.RawMessage) (*configmgr.SaveResult, error) {
	f.saveBody = append(json.RawMessage(nil), raw...)
	return f.saveResult, f.saveErr
}

func TestGetConfigReturnsLoadResult(t *testing.T) {
	service := &fakeConfigService{loadResult: &configmgr.LoadResult{
		Config:         json.RawMessage(`{"ok":true}`),
		ConfigPath:     "/etc/sing-box/config.json",
		Source:         "/etc/sing-box/config.json",
		LoadedAt:       time.Unix(1, 0),
		OutboundCount:  2,
		RouteRuleCount: 3,
		RouteFinal:     "direct",
	}}
	handler := New(service, testStaticFS())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"routeFinal":"direct"`) {
		t.Fatalf("body missing load result: %s", rec.Body.String())
	}
}

func TestSaveConfigPassesRawConfigObject(t *testing.T) {
	service := &fakeConfigService{saveResult: &configmgr.SaveResult{BackupPath: "/tmp/config.bak"}}
	handler := New(service, testStaticFS())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", strings.NewReader(`{"config":{"x":1}}`))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if string(service.saveBody) != `{"x":1}` {
		t.Fatalf("saveBody = %s, want config object", service.saveBody)
	}
}

func TestSaveConfigMapsCheckFailureTo422(t *testing.T) {
	service := &fakeConfigService{saveResult: &configmgr.SaveResult{}, saveErr: configmgr.ErrCheckFailed}
	handler := New(service, testStaticFS())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", strings.NewReader(`{"config":{"x":1}}`))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestStaticIndexServed(t *testing.T) {
	handler := New(&fakeConfigService{}, testStaticFS())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), "kt-proxy") {
		t.Fatalf("status=%d body=%s", rec.Code, string(body))
	}
}

func testStaticFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {Data: []byte(`<html><body>kt-proxy</body></html>`)},
	}
}

func TestLoadErrorReturns500(t *testing.T) {
	service := &fakeConfigService{loadErr: errors.New("boom")}
	handler := New(service, testStaticFS())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
```

- [ ] **Step 2: Run server tests to verify they fail**

Run: `go test ./internal/server`

Expected: FAIL because `New` is undefined.

- [ ] **Step 3: Implement HTTP server**

Create `internal/server/server.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"

	"kt-proxy/internal/configmgr"
)

type ConfigService interface {
	Load(ctx context.Context) (*configmgr.LoadResult, error)
	Save(ctx context.Context, raw json.RawMessage) (*configmgr.SaveResult, error)
}

func New(service ConfigService, staticFS fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		result, err := service.Load(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/config/save", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Config json.RawMessage `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.Config) == 0 {
			writeError(w, http.StatusBadRequest, errors.New("request body must contain config"))
			return
		}
		result, err := service.Save(r.Context(), payload.Config)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, configmgr.ErrInvalidJSON) {
				status = http.StatusBadRequest
			}
			if errors.Is(err, configmgr.ErrCheckFailed) {
				status = http.StatusUnprocessableEntity
			}
			writeJSON(w, status, map[string]any{
				"error":  err.Error(),
				"result": result,
			})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.Handle("/", http.FileServer(http.FS(staticFS)))
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
```

- [ ] **Step 4: Add entry point and placeholder app shell**

Create `web/static/index.html`:

```html
<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>kt-proxy</title>
  </head>
  <body>
    <main id="app">kt-proxy</main>
  </body>
</html>
```

Create `main.go`:

```go
package main

import (
	"embed"
	"log"
	"net/http"
	"os"

	"kt-proxy/internal/configmgr"
	"kt-proxy/internal/server"
)

//go:embed web/static/*
var embeddedStatic embed.FS

func main() {
	addr := env("KT_PROXY_ADDR", ":8090")
	manager := configmgr.New(configmgr.Paths{
		ConfigPath:  env("SING_BOX_CONFIG_PATH", "/etc/sing-box/config.json"),
		ExamplePath: env("SING_BOX_EXAMPLE_PATH", "sing-box-config-example.json"),
	}, configmgr.Commands{
		SingBox:   env("SING_BOX_BIN", "sing-box"),
		Systemctl: env("SYSTEMCTL_BIN", "systemctl"),
	}, nil)

	staticFS, err := fs.Sub(embeddedStatic, "web/static")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}
	handler := server.New(manager, staticFS)
	log.Printf("kt-proxy listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func env(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
```

- [ ] **Step 5: Fix entry point imports**

Replace `main.go` with:

```go
package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"

	"kt-proxy/internal/configmgr"
	"kt-proxy/internal/server"
)

//go:embed web/static/*
var embeddedStatic embed.FS

func main() {
	addr := env("KT_PROXY_ADDR", ":8090")
	manager := configmgr.New(configmgr.Paths{
		ConfigPath:  env("SING_BOX_CONFIG_PATH", "/etc/sing-box/config.json"),
		ExamplePath: env("SING_BOX_EXAMPLE_PATH", "sing-box-config-example.json"),
	}, configmgr.Commands{
		SingBox:   env("SING_BOX_BIN", "sing-box"),
		Systemctl: env("SYSTEMCTL_BIN", "systemctl"),
	}, nil)

	staticFS, err := fs.Sub(embeddedStatic, "web/static")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}
	handler := server.New(manager, staticFS)
	log.Printf("kt-proxy listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func env(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
```

- [ ] **Step 6: Run all Go tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 7: Commit if git exists**

Run only if `git rev-parse --is-inside-work-tree` succeeds:

```bash
git add main.go internal/server/server.go internal/server/server_test.go web/static/index.html
git commit -m "feat: add web api server"
```

---

### Task 3: Frontend Application

**Files:**
- Modify: `web/static/index.html`
- Create: `web/static/styles.css`
- Create: `web/static/app.js`
- Modify: `main.go` if embed path changed in Task 2

**Interfaces:**
- Consumes: `GET /api/config`
- Consumes: `POST /api/config/save` with body `{ "config": <object> }`
- Produces: usable browser UI for Overview, Outbounds, Route Rules, and Full JSON.

- [ ] **Step 1: Replace HTML shell**

Use this structure in `web/static/index.html`:

```html
<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>kt-proxy</title>
    <link rel="stylesheet" href="/styles.css" />
  </head>
  <body>
    <main class="app">
      <header class="topbar">
        <div>
          <h1>kt-proxy</h1>
          <p id="subtitle">sing-box 配置管理</p>
        </div>
        <div class="actions">
          <button id="reloadBtn" type="button">重新加载</button>
          <button id="saveBtn" type="button" class="primary">检查并保存</button>
        </div>
      </header>

      <section id="alert" class="alert hidden"></section>

      <nav class="tabs" aria-label="配置视图">
        <button class="tab active" type="button" data-tab="overview">总览</button>
        <button class="tab" type="button" data-tab="outbounds">Outbounds</button>
        <button class="tab" type="button" data-tab="rules">Route Rules</button>
        <button class="tab" type="button" data-tab="json">完整 JSON</button>
      </nav>

      <section id="overview" class="panel active"></section>
      <section id="outbounds" class="panel"></section>
      <section id="rules" class="panel"></section>
      <section id="json" class="panel"></section>
    </main>

    <dialog id="editorDialog">
      <form method="dialog" class="dialog">
        <header>
          <h2 id="dialogTitle">编辑</h2>
          <button type="button" id="closeDialogBtn" class="icon-button">×</button>
        </header>
        <div id="dialogBody"></div>
        <footer>
          <button type="button" id="cancelDialogBtn">取消</button>
          <button type="button" id="confirmDialogBtn" class="primary">保存</button>
        </footer>
      </form>
    </dialog>

    <script src="/app.js"></script>
  </body>
</html>
```

- [ ] **Step 2: Add CSS**

Create `web/static/styles.css` with restrained dashboard styling:

```css
:root {
  color-scheme: light;
  --bg: #f6f7f9;
  --surface: #ffffff;
  --text: #1f2937;
  --muted: #667085;
  --line: #d9dee7;
  --primary: #2563eb;
  --primary-dark: #1d4ed8;
  --danger: #dc2626;
  --ok: #047857;
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
}

button,
input,
select,
textarea {
  font: inherit;
}

button {
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
  padding: 8px 12px;
  cursor: pointer;
}

button:hover {
  border-color: #9aa4b2;
}

button.primary {
  border-color: var(--primary);
  background: var(--primary);
  color: white;
}

button.primary:hover {
  background: var(--primary-dark);
}

button.danger {
  border-color: #fecaca;
  color: var(--danger);
}

.app {
  width: min(1440px, calc(100vw - 32px));
  margin: 0 auto;
  padding: 20px 0 40px;
}

.topbar {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  margin-bottom: 16px;
}

.topbar h1 {
  margin: 0;
  font-size: 24px;
}

.topbar p {
  margin: 4px 0 0;
  color: var(--muted);
}

.actions,
.row-actions,
.toolbar {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--line);
  margin-bottom: 16px;
}

.tab {
  border: 0;
  border-bottom: 2px solid transparent;
  border-radius: 0;
  background: transparent;
}

.tab.active {
  border-bottom-color: var(--primary);
  color: var(--primary);
}

.panel {
  display: none;
}

.panel.active {
  display: block;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}

.metric {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 14px;
}

.metric span {
  display: block;
  color: var(--muted);
  font-size: 13px;
}

.metric strong {
  display: block;
  margin-top: 6px;
  overflow-wrap: anywhere;
}

.table-wrap {
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
}

table {
  width: 100%;
  border-collapse: collapse;
  min-width: 920px;
}

th,
td {
  border-bottom: 1px solid var(--line);
  padding: 10px;
  text-align: left;
  vertical-align: top;
}

th {
  color: var(--muted);
  font-size: 13px;
  background: #fbfcfe;
}

td {
  font-size: 14px;
}

.toolbar {
  justify-content: space-between;
  margin-bottom: 10px;
}

.search {
  min-width: 260px;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 8px 10px;
}

.json-editor,
.object-editor {
  width: 100%;
  min-height: 480px;
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  line-height: 1.45;
  resize: vertical;
}

.object-editor {
  min-height: 220px;
}

.alert {
  border-radius: 8px;
  border: 1px solid var(--line);
  padding: 12px;
  margin-bottom: 16px;
  background: var(--surface);
  white-space: pre-wrap;
}

.alert.error {
  border-color: #fecaca;
  color: #991b1b;
  background: #fff7f7;
}

.alert.ok {
  border-color: #bbf7d0;
  color: var(--ok);
  background: #f0fdf4;
}

.hidden {
  display: none;
}

dialog {
  width: min(860px, calc(100vw - 32px));
  border: 0;
  border-radius: 8px;
  padding: 0;
}

dialog::backdrop {
  background: rgba(15, 23, 42, 0.35);
}

.dialog {
  padding: 16px;
}

.dialog header,
.dialog footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.dialog h2 {
  margin: 0;
  font-size: 18px;
}

.dialog footer {
  justify-content: flex-end;
  margin-top: 14px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
  margin-top: 14px;
}

label {
  display: grid;
  gap: 6px;
  color: var(--muted);
  font-size: 13px;
}

input,
select,
textarea {
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 8px 10px;
  background: white;
  color: var(--text);
}

.icon-button {
  width: 36px;
  height: 36px;
  padding: 0;
}

@media (max-width: 720px) {
  .app {
    width: min(100vw - 20px, 720px);
  }

  .topbar {
    align-items: flex-start;
    flex-direction: column;
  }

  .actions,
  .actions button {
    width: 100%;
  }

  .tabs {
    overflow-x: auto;
  }

  .toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .search {
    min-width: 0;
    width: 100%;
  }
}
```

- [ ] **Step 3: Add JavaScript state and rendering**

Create `web/static/app.js` with complete frontend behavior:

```javascript
const state = {
  config: null,
  meta: null,
  activeTab: "overview",
  outboundQuery: "",
  ruleQuery: "",
  editor: null,
};

const el = (id) => document.getElementById(id);

document.addEventListener("DOMContentLoaded", () => {
  document.querySelectorAll(".tab").forEach((button) => {
    button.addEventListener("click", () => switchTab(button.dataset.tab));
  });
  el("reloadBtn").addEventListener("click", loadConfig);
  el("saveBtn").addEventListener("click", saveConfig);
  el("closeDialogBtn").addEventListener("click", closeDialog);
  el("cancelDialogBtn").addEventListener("click", closeDialog);
  el("confirmDialogBtn").addEventListener("click", confirmDialog);
  loadConfig();
});

async function loadConfig() {
  showAlert("正在加载配置...", "");
  try {
    const response = await fetch("/api/config");
    const body = await response.json();
    if (!response.ok) throw new Error(body.error || "加载失败");
    state.config = body.config;
    state.meta = body;
    render();
    if (body.fallback) {
      showAlert(`无法读取 ${body.configPath}，当前展示示例配置：${body.source}\n${body.loadError || ""}`, "error");
    } else {
      hideAlert();
    }
  } catch (error) {
    showAlert(error.message, "error");
  }
}

async function saveConfig() {
  if (!state.config) return;
  const jsonText = el("fullJson")?.value;
  if (state.activeTab === "json" && jsonText) {
    try {
      state.config = JSON.parse(jsonText);
    } catch (error) {
      showAlert(`JSON 解析失败：${error.message}`, "error");
      return;
    }
  }
  showAlert("正在运行 sing-box check...", "");
  try {
    const response = await fetch("/api/config/save", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({config: state.config}),
    });
    const body = await response.json();
    if (!response.ok) {
      const detail = body.result ? formatCommandResult(body.result) : "";
      throw new Error(`${body.error || "保存失败"}\n${detail}`);
    }
    showAlert(`保存成功\n备份：${body.backupPath || "无"}\n${formatCommandResult(body)}`, "ok");
    await loadConfig();
  } catch (error) {
    showAlert(error.message, "error");
  }
}

function render() {
  renderOverview();
  renderOutbounds();
  renderRules();
  renderJson();
}

function switchTab(tab) {
  state.activeTab = tab;
  document.querySelectorAll(".tab").forEach((button) => button.classList.toggle("active", button.dataset.tab === tab));
  document.querySelectorAll(".panel").forEach((panel) => panel.classList.toggle("active", panel.id === tab));
  if (tab === "json") renderJson();
}

function renderOverview() {
  const meta = state.meta || {};
  el("overview").innerHTML = `
    <div class="summary-grid">
      ${metric("配置路径", meta.configPath || "")}
      ${metric("当前来源", meta.source || "")}
      ${metric("加载时间", meta.loadedAt ? new Date(meta.loadedAt).toLocaleString() : "")}
      ${metric("Outbounds", String(meta.outboundCount ?? outboundList().length))}
      ${metric("Route Rules", String(meta.routeRuleCount ?? ruleList().length))}
      ${metric("route.final", meta.routeFinal || valueAt(state.config, ["route", "final"]) || "")}
    </div>
  `;
}

function renderOutbounds() {
  const rows = outboundList()
    .map((item, index) => ({item, index}))
    .filter(({item}) => JSON.stringify(item).toLowerCase().includes(state.outboundQuery.toLowerCase()));
  el("outbounds").innerHTML = `
    <div class="toolbar">
      <input class="search" id="outboundSearch" placeholder="搜索 tag、server、type" value="${escapeHtml(state.outboundQuery)}" />
      <button type="button" id="addOutboundBtn" class="primary">新增 outbound</button>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th>#</th><th>tag</th><th>type</th><th>server</th><th>port</th><th>network</th><th>操作</th></tr></thead>
        <tbody>
          ${rows.map(({item, index}) => `
            <tr>
              <td>${index + 1}</td>
              <td>${escapeHtml(item.tag || "")}</td>
              <td>${escapeHtml(item.type || "")}</td>
              <td>${escapeHtml(item.server || "")}</td>
              <td>${escapeHtml(item.server_port ?? "")}</td>
              <td>${escapeHtml(item.network || "")}</td>
              <td><div class="row-actions">
                <button type="button" data-action="edit-outbound" data-index="${index}">编辑</button>
                <button type="button" data-action="dup-outbound" data-index="${index}">复制</button>
                <button type="button" class="danger" data-action="del-outbound" data-index="${index}">删除</button>
              </div></td>
            </tr>
          `).join("")}
        </tbody>
      </table>
    </div>
  `;
  el("outboundSearch").addEventListener("input", (event) => {
    state.outboundQuery = event.target.value;
    renderOutbounds();
  });
  el("addOutboundBtn").addEventListener("click", () => openObjectEditor("新增 outbound", {type: "socks", tag: "", server: "", server_port: 1080, version: "5", network: "tcp"}, (value) => {
    ensureArray(["outbounds"]).push(value);
    render();
  }));
  bindTableActions();
}

function renderRules() {
  const rows = ruleList()
    .map((item, index) => ({item, index}))
    .filter(({item}) => JSON.stringify(item).toLowerCase().includes(state.ruleQuery.toLowerCase()));
  el("rules").innerHTML = `
    <div class="toolbar">
      <input class="search" id="ruleSearch" placeholder="搜索 action、outbound、ip_cidr、domain" value="${escapeHtml(state.ruleQuery)}" />
      <button type="button" id="addRuleBtn" class="primary">新增 rule</button>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th>#</th><th>action</th><th>outbound</th><th>protocol</th><th>match</th><th>操作</th></tr></thead>
        <tbody>
          ${rows.map(({item, index}) => `
            <tr>
              <td>${index + 1}</td>
              <td>${escapeHtml(item.action || "")}</td>
              <td>${escapeHtml(item.outbound || "")}</td>
              <td>${escapeHtml(item.protocol || "")}</td>
              <td>${escapeHtml(matchSummary(item))}</td>
              <td><div class="row-actions">
                <button type="button" data-action="up-rule" data-index="${index}">上移</button>
                <button type="button" data-action="down-rule" data-index="${index}">下移</button>
                <button type="button" data-action="edit-rule" data-index="${index}">编辑</button>
                <button type="button" data-action="dup-rule" data-index="${index}">复制</button>
                <button type="button" class="danger" data-action="del-rule" data-index="${index}">删除</button>
              </div></td>
            </tr>
          `).join("")}
        </tbody>
      </table>
    </div>
  `;
  el("ruleSearch").addEventListener("input", (event) => {
    state.ruleQuery = event.target.value;
    renderRules();
  });
  el("addRuleBtn").addEventListener("click", () => openObjectEditor("新增 rule", {ip_cidr: [], outbound: ""}, (value) => {
    ensureArray(["route", "rules"]).push(value);
    render();
  }));
  bindTableActions();
}

function renderJson() {
  el("json").innerHTML = `
    <textarea id="fullJson" class="json-editor" spellcheck="false">${escapeHtml(JSON.stringify(state.config, null, 2))}</textarea>
  `;
  el("fullJson").addEventListener("change", (event) => {
    try {
      state.config = JSON.parse(event.target.value);
      renderOverview();
      renderOutbounds();
      renderRules();
      hideAlert();
    } catch (error) {
      showAlert(`JSON 解析失败：${error.message}`, "error");
    }
  });
}

function bindTableActions() {
  document.querySelectorAll("[data-action]").forEach((button) => {
    button.addEventListener("click", () => {
      const index = Number(button.dataset.index);
      const action = button.dataset.action;
      if (action.endsWith("outbound")) handleOutboundAction(action, index);
      if (action.endsWith("rule")) handleRuleAction(action, index);
    });
  });
}

function handleOutboundAction(action, index) {
  const list = outboundList();
  if (action === "edit-outbound") {
    openObjectEditor("编辑 outbound", list[index], (value) => {
      list[index] = value;
      render();
    });
  }
  if (action === "dup-outbound") {
    list.splice(index + 1, 0, structuredClone(list[index]));
    render();
  }
  if (action === "del-outbound" && confirm("删除这个 outbound？")) {
    list.splice(index, 1);
    render();
  }
}

function handleRuleAction(action, index) {
  const list = ruleList();
  if (action === "edit-rule") {
    openObjectEditor("编辑 rule", list[index], (value) => {
      list[index] = value;
      render();
    });
  }
  if (action === "dup-rule") {
    list.splice(index + 1, 0, structuredClone(list[index]));
    render();
  }
  if (action === "del-rule" && confirm("删除这个 rule？")) {
    list.splice(index, 1);
    render();
  }
  if (action === "up-rule" && index > 0) {
    [list[index - 1], list[index]] = [list[index], list[index - 1]];
    render();
  }
  if (action === "down-rule" && index < list.length - 1) {
    [list[index + 1], list[index]] = [list[index], list[index + 1]];
    render();
  }
}

function openObjectEditor(title, value, onSave) {
  state.editor = {onSave};
  el("dialogTitle").textContent = title;
  el("dialogBody").innerHTML = `<textarea id="objectJson" class="object-editor" spellcheck="false">${escapeHtml(JSON.stringify(value, null, 2))}</textarea>`;
  el("editorDialog").showModal();
}

function confirmDialog() {
  try {
    const value = JSON.parse(el("objectJson").value);
    state.editor.onSave(value);
    closeDialog();
  } catch (error) {
    showAlert(`对象 JSON 解析失败：${error.message}`, "error");
  }
}

function closeDialog() {
  el("editorDialog").close();
  state.editor = null;
}

function outboundList() {
  return Array.isArray(state.config?.outbounds) ? state.config.outbounds : [];
}

function ruleList() {
  return Array.isArray(state.config?.route?.rules) ? state.config.route.rules : [];
}

function ensureArray(path) {
  let cursor = state.config;
  for (let i = 0; i < path.length - 1; i += 1) {
    const key = path[i];
    if (!cursor[key] || typeof cursor[key] !== "object") cursor[key] = {};
    cursor = cursor[key];
  }
  const last = path[path.length - 1];
  if (!Array.isArray(cursor[last])) cursor[last] = [];
  return cursor[last];
}

function valueAt(root, path) {
  return path.reduce((cursor, key) => cursor && cursor[key], root);
}

function matchSummary(item) {
  for (const key of ["ip_cidr", "domain", "domain_suffix", "domain_keyword", "rule_set"]) {
    if (item[key]) return `${key}: ${Array.isArray(item[key]) ? item[key].join(", ") : item[key]}`;
  }
  return JSON.stringify(item);
}

function metric(label, value) {
  return `<div class="metric"><span>${escapeHtml(label)}</span><strong>${escapeHtml(value)}</strong></div>`;
}

function showAlert(message, kind) {
  const alert = el("alert");
  alert.textContent = message;
  alert.className = `alert ${kind || ""}`;
}

function hideAlert() {
  el("alert").className = "alert hidden";
}

function formatCommandResult(result) {
  const parts = [];
  if (result.check) parts.push(`check stdout:\n${result.check.stdout || ""}\ncheck stderr:\n${result.check.stderr || ""}`);
  if (result.reload) parts.push(`reload stdout:\n${result.reload.stdout || ""}\nreload stderr:\n${result.reload.stderr || ""}`);
  return parts.join("\n");
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}
```

- [ ] **Step 4: Run Go tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 5: Build binary**

Run: `go build -o kt-proxy .`

Expected: PASS and local binary generated if the command uses default output behavior.

- [ ] **Step 6: Commit if git exists**

Run only if `git rev-parse --is-inside-work-tree` succeeds:

```bash
git add web/static/index.html web/static/styles.css web/static/app.js main.go
git commit -m "feat: add sing-box web ui"
```

---

### Task 4: Documentation and Local Verification

**Files:**
- Create: `README.md`

**Interfaces:**
- Consumes: runnable binary from prior tasks.
- Produces: user-facing instructions for build, local sample run, Ubuntu deployment, and privilege requirements.

- [ ] **Step 1: Create README**

Create `README.md`:

```markdown
# kt-proxy

`kt-proxy` is a small unauthenticated web manager for `sing-box` configuration on Ubuntu.

It reads `/etc/sing-box/config.json`, displays editable `outbounds` and `route.rules`, offers a full JSON editor, and saves only after `sing-box check` succeeds. After writing the checked config, it runs `systemctl reload sing-box`.

## Build

```bash
go build -o kt-proxy .
```

## Run for local development

Use the sample config as the write target so the real system config is not touched:

```bash
SING_BOX_CONFIG_PATH=./sing-box-config-example.json \
SING_BOX_EXAMPLE_PATH=./sing-box-config-example.json \
SING_BOX_BIN=/bin/true \
SYSTEMCTL_BIN=/bin/true \
KT_PROXY_ADDR=:8090 \
./kt-proxy
```

Open `http://localhost:8090`.

## Run on Ubuntu

The default runtime values are:

- `KT_PROXY_ADDR=:8090`
- `SING_BOX_CONFIG_PATH=/etc/sing-box/config.json`
- `SING_BOX_EXAMPLE_PATH=sing-box-config-example.json`
- `SING_BOX_BIN=sing-box`
- `SYSTEMCTL_BIN=systemctl`

The process needs permission to:

- Read and write `/etc/sing-box/config.json`
- Create a backup next to `/etc/sing-box/config.json`
- Run `sing-box check -c <temp-file>`
- Run `systemctl reload sing-box`

## Example systemd unit

```ini
[Unit]
Description=kt-proxy sing-box web manager
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/kt-proxy
Environment=KT_PROXY_ADDR=:8090
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Security

The first version has no login, no TLS, and no CSRF protection. Run it only in a trusted environment.
```

- [ ] **Step 2: Run tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 3: Build binary**

Run: `go build -o kt-proxy .`

Expected: PASS.

- [ ] **Step 4: Start local development server**

Run:

```bash
SING_BOX_CONFIG_PATH=./sing-box-config-example.json SING_BOX_EXAMPLE_PATH=./sing-box-config-example.json SING_BOX_BIN=/bin/true SYSTEMCTL_BIN=/bin/true KT_PROXY_ADDR=:8090 ./kt-proxy
```

Expected: log output contains `kt-proxy listening on :8090`.

- [ ] **Step 5: Verify API**

In another shell, run:

```bash
curl -s http://localhost:8090/api/config
```

Expected: JSON response includes `"configPath":"./sing-box-config-example.json"` and positive `outboundCount`.

- [ ] **Step 6: Commit if git exists**

Run only if `git rev-parse --is-inside-work-tree` succeeds:

```bash
git add README.md
git commit -m "docs: add kt-proxy usage"
```
