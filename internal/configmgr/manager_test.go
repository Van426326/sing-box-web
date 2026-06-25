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
	if content := mustRead(t, configPath); strings.TrimSpace(content) != "{\n  \"new\": true\n}" {
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
