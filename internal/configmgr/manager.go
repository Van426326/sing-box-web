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
	_ = ctx
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
