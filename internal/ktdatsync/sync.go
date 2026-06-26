package ktdatsync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"kt-proxy/internal/configmgr"
)

var (
	ErrMissingConfig = errors.New("missing kt-dat configuration")
	ErrInvalidRepo   = errors.New("invalid kt-dat repository")
	ErrGitHub        = errors.New("github api error")
	ErrConflict      = errors.New("github content conflict")
)

type Config struct {
	Repo    string
	Branch  string
	Path    string
	Token   string
	APIBase string
}

type ConfigLoader interface {
	Load(ctx context.Context) (*configmgr.LoadResult, error)
}

type Service struct {
	config       Config
	configLoader ConfigLoader
	httpClient   *http.Client
}

type Result struct {
	Target    string `json:"target"`
	Changed   bool   `json:"changed"`
	CIDRCount int    `json:"cidrCount"`
	CommitSHA string `json:"commitSha,omitempty"`
	CommitURL string `json:"commitUrl,omitempty"`
	Message   string `json:"message"`
}

type contentResponse struct {
	SHA      string `json:"sha"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type putContentRequest struct {
	Message string `json:"message"`
	Content string `json:"content"`
	Branch  string `json:"branch"`
	SHA     string `json:"sha,omitempty"`
}

type putContentResponse struct {
	Commit struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
	} `json:"commit"`
}

func New(config Config, configLoader ConfigLoader, httpClient *http.Client) *Service {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	config = withDefaults(config)
	return &Service{config: config, configLoader: configLoader, httpClient: httpClient}
}

func (s *Service) Sync(ctx context.Context) (*Result, error) {
	missing := missingConfig(s.config)
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrMissingConfig, strings.Join(missing, ", "))
	}
	owner, repo, err := splitRepo(s.config.Repo)
	if err != nil {
		return nil, err
	}
	loaded, err := s.configLoader.Load(ctx)
	if err != nil {
		return nil, err
	}
	cidrs, err := ExtractIPCidrs(loaded.Config)
	if err != nil {
		return nil, err
	}
	target := fmt.Sprintf("%s:%s:%s", s.config.Repo, s.config.Branch, s.config.Path)

	remote, exists, err := s.getContent(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	merged := cidrs
	if exists {
		remoteContent, err := decodeContent(remote)
		if err != nil {
			return nil, err
		}
		existing := ParseKTText(remoteContent)
		merged = MergeCIDRs(existing, cidrs)
		if len(merged) == len(existing) {
			result := &Result{
				Target:    target,
				CIDRCount: len(merged),
			}
			result.Message = "kt-dat 已是最新，无需提交"
			return result, nil
		}
	}

	content := RenderKTText(merged)
	commit, err := s.putContent(ctx, owner, repo, content, remote.SHA)
	if err != nil {
		return nil, err
	}
	return &Result{
		Target:    target,
		Changed:   true,
		CIDRCount: len(merged),
		CommitSHA: commit.Commit.SHA,
		CommitURL: commit.Commit.HTMLURL,
		Message:   fmt.Sprintf("已同步 %d 个 IP 到 kt-dat", len(merged)),
	}, nil
}

func ExtractIPCidrs(raw json.RawMessage) ([]string, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	route, _ := root["route"].(map[string]any)
	rules, _ := route["rules"].([]any)
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, item := range rules {
		rule, _ := item.(map[string]any)
		switch value := rule["ip_cidr"].(type) {
		case string:
			result = appendUnique(result, seen, value)
		case []any:
			for _, entry := range value {
				if text, ok := entry.(string); ok {
					result = appendUnique(result, seen, text)
				}
			}
		}
	}
	return result, nil
}

func RenderKTText(cidrs []string) string {
	if len(cidrs) == 0 {
		return ""
	}
	return strings.Join(cidrs, "\n") + "\n"
}

func ParseKTText(text string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, line := range strings.Split(text, "\n") {
		result = appendUnique(result, seen, line)
	}
	return result
}

func MergeCIDRs(existing []string, incoming []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	result := make([]string, 0, len(existing)+len(incoming))
	for _, item := range existing {
		result = appendUnique(result, seen, item)
	}
	for _, item := range incoming {
		result = appendUnique(result, seen, item)
	}
	return result
}

func (s *Service) getContent(ctx context.Context, owner string, repo string) (contentResponse, bool, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		strings.TrimRight(s.config.APIBase, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
		escapeContentPath(s.config.Path),
		url.QueryEscape(s.config.Branch),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return contentResponse{}, false, fmt.Errorf("%w: %v", ErrGitHub, err)
	}
	s.setGitHubHeaders(req)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return contentResponse{}, false, fmt.Errorf("%w: %v", ErrGitHub, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return contentResponse{}, false, fmt.Errorf("%w: %v", ErrGitHub, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return contentResponse{}, false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return contentResponse{}, false, githubError(resp.StatusCode, body)
	}
	var payload contentResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return contentResponse{}, false, fmt.Errorf("%w: %v", ErrGitHub, err)
	}
	return payload, true, nil
}

func (s *Service) putContent(ctx context.Context, owner string, repo string, content string, sha string) (putContentResponse, error) {
	payload := putContentRequest{
		Message: "chore: update kt cidr list",
		Content: base64.StdEncoding.EncodeToString([]byte(content)),
		Branch:  s.config.Branch,
		SHA:     sha,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return putContentResponse{}, err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s",
		strings.TrimRight(s.config.APIBase, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
		escapeContentPath(s.config.Path),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return putContentResponse{}, fmt.Errorf("%w: %v", ErrGitHub, err)
	}
	s.setGitHubHeaders(req)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return putContentResponse{}, fmt.Errorf("%w: %v", ErrGitHub, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return putContentResponse{}, fmt.Errorf("%w: %v", ErrGitHub, err)
	}
	if resp.StatusCode == http.StatusConflict {
		return putContentResponse{}, fmt.Errorf("%w: remote kt.txt changed, please retry", ErrConflict)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return putContentResponse{}, githubError(resp.StatusCode, respBody)
	}
	var result putContentResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return putContentResponse{}, fmt.Errorf("%w: %v", ErrGitHub, err)
	}
	return result, nil
}

func (s *Service) setGitHubHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+s.config.Token)
}

func withDefaults(config Config) Config {
	if strings.TrimSpace(config.Repo) == "" {
		config.Repo = "Van426326/kt-dat"
	}
	if strings.TrimSpace(config.Branch) == "" {
		config.Branch = "main"
	}
	if strings.TrimSpace(config.Path) == "" {
		config.Path = "kt.txt"
	}
	if strings.TrimSpace(config.APIBase) == "" {
		config.APIBase = "https://api.github.com"
	}
	return config
}

func missingConfig(config Config) []string {
	missing := make([]string, 0, 1)
	if strings.TrimSpace(config.Token) == "" {
		missing = append(missing, "KTDAT_TOKEN")
	}
	sort.Strings(missing)
	return missing
}

func splitRepo(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("%w: KTDAT_REPO must be owner/repo", ErrInvalidRepo)
	}
	return parts[0], parts[1], nil
}

func decodeContent(response contentResponse) (string, error) {
	if response.Encoding != "" && response.Encoding != "base64" {
		return "", fmt.Errorf("%w: unsupported content encoding %q", ErrGitHub, response.Encoding)
	}
	compact := strings.ReplaceAll(response.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrGitHub, err)
	}
	return string(decoded), nil
}

func githubError(status int, body []byte) error {
	var payload struct {
		Message string `json:"message"`
	}
	message := strings.TrimSpace(string(body))
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Message) != "" {
		message = payload.Message
	}
	return fmt.Errorf("%w: http %d: %s", ErrGitHub, status, message)
}

func appendUnique(result []string, seen map[string]struct{}, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return result
	}
	if _, ok := seen[value]; ok {
		return result
	}
	seen[value] = struct{}{}
	return append(result, value)
}

func escapeContentPath(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
