package daedsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"kt-proxy/internal/configmgr"
)

var (
	ErrMissingConfig       = errors.New("missing daed configuration")
	ErrNoSelectedRouting   = errors.New("no selected daed routing")
	ErrMarkerNotFound      = errors.New("daed routing marker not found")
	ErrTargetBlockNotFound = errors.New("daed singbox dip block not found")
	ErrGraphQL             = errors.New("daed graphql error")
)

type Config struct {
	GraphQLURL    string
	Authorization string
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
	RoutingID     string   `json:"routingId"`
	RoutingName   string   `json:"routingName"`
	Changed       bool     `json:"changed"`
	ExistingCount int      `json:"existingCount"`
	SourceCount   int      `json:"sourceCount"`
	Added         []string `json:"added"`
	Message       string   `json:"message"`
}

type MergeResult struct {
	Changed       bool
	ExistingCount int
	SourceCount   int
	Added         []string
}

func New(config Config, configLoader ConfigLoader, httpClient *http.Client) *Service {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Service{config: config, configLoader: configLoader, httpClient: httpClient}
}

func (s *Service) Sync(ctx context.Context) (*Result, error) {
	missing := missingConfig(s.config)
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrMissingConfig, strings.Join(missing, ", "))
	}
	loaded, err := s.configLoader.Load(ctx)
	if err != nil {
		return nil, err
	}
	source, err := ExtractIPCidrs(loaded.Config)
	if err != nil {
		return nil, err
	}
	routing, err := s.selectedRouting(ctx)
	if err != nil {
		return nil, err
	}
	updated, merge, err := MergeRoutingBlock(routing.Routing.String, source)
	if err != nil {
		return nil, err
	}
	result := &Result{
		RoutingID:     routing.ID,
		RoutingName:   routing.Name,
		Changed:       merge.Changed,
		ExistingCount: merge.ExistingCount,
		SourceCount:   merge.SourceCount,
		Added:         merge.Added,
	}
	if !merge.Changed {
		result.Message = "Daed 已是最新，无需更新"
		return result, nil
	}
	if err := s.updateRouting(ctx, routing.ID, updated); err != nil {
		return result, err
	}
	if err := s.run(ctx); err != nil {
		return result, err
	}
	result.Message = fmt.Sprintf("已同步 %d 个 IP 到 Daed", len(merge.Added))
	return result, nil
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

func MergeRoutingBlock(routing string, source []string) (string, MergeResult, error) {
	markerIndex := strings.Index(routing, "# 家 & kt")
	if markerIndex < 0 {
		return "", MergeResult{}, ErrMarkerNotFound
	}
	blockStart := strings.Index(routing[markerIndex:], "dip(")
	if blockStart < 0 {
		return "", MergeResult{}, ErrTargetBlockNotFound
	}
	blockStart += markerIndex
	openParen := blockStart + len("dip(") - 1
	closeParen := findMatchingParen(routing, openParen)
	if closeParen < 0 {
		return "", MergeResult{}, ErrTargetBlockNotFound
	}
	lineEnd := strings.IndexByte(routing[closeParen:], '\n')
	blockEnd := len(routing)
	if lineEnd >= 0 {
		blockEnd = closeParen + lineEnd
	}
	suffix := routing[closeParen:blockEnd]
	if !strings.Contains(suffix, "-> singbox") {
		return "", MergeResult{}, ErrTargetBlockNotFound
	}

	existing := parseDipEntries(routing[openParen+1 : closeParen])
	seen := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		seen[item] = struct{}{}
	}
	added := make([]string, 0)
	for _, item := range source {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		existing = append(existing, item)
		added = append(added, item)
	}
	merge := MergeResult{
		Changed:       len(added) > 0,
		ExistingCount: len(existing) - len(added),
		SourceCount:   len(source),
		Added:         added,
	}
	if !merge.Changed {
		return routing, merge, nil
	}
	replacement := "dip(\n" + strings.Join(existing, ",\n") + "\n) -> singbox"
	return routing[:blockStart] + replacement + routing[blockEnd:], merge, nil
}

type graphQLRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
	OperationName string         `json:"operationName,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage  `json:"data"`
	Errors []map[string]any `json:"errors"`
}

type routingItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Selected bool   `json:"selected"`
	Routing  struct {
		String string `json:"string"`
	} `json:"routing"`
}

func (s *Service) selectedRouting(ctx context.Context) (routingItem, error) {
	var payload struct {
		Routings []routingItem `json:"routings"`
	}
	err := s.graphQL(ctx, graphQLRequest{
		Query:         "query Routings {\n  routings {\n    id\n    name\n    selected\n    routing {\n      string\n    }\n  }\n}",
		OperationName: "Routings",
	}, &payload)
	if err != nil {
		return routingItem{}, err
	}
	for _, routing := range payload.Routings {
		if routing.Selected {
			return routing, nil
		}
	}
	return routingItem{}, ErrNoSelectedRouting
}

func (s *Service) updateRouting(ctx context.Context, id string, routing string) error {
	var payload struct {
		UpdateRouting struct {
			ID string `json:"id"`
		} `json:"updateRouting"`
	}
	return s.graphQL(ctx, graphQLRequest{
		Query:         "mutation UpdateRouting($id: ID!, $routing: String!) {\n  updateRouting(id: $id, routing: $routing) {\n    id\n  }\n}",
		Variables:     map[string]any{"id": id, "routing": routing},
		OperationName: "UpdateRouting",
	}, &payload)
}

func (s *Service) run(ctx context.Context) error {
	var payload struct {
		Run json.RawMessage `json:"run"`
	}
	return s.graphQL(ctx, graphQLRequest{
		Query:         "mutation Run($dry: Boolean!) {\n  run(dry: $dry)\n}",
		Variables:     map[string]any{"dry": false},
		OperationName: "Run",
	}, &payload)
}

func (s *Service) graphQL(ctx context.Context, requestPayload graphQLRequest, dataTarget any) error {
	body, err := json.Marshal(requestPayload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.GraphQLURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrGraphQL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", s.config.Authorization)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrGraphQL, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrGraphQL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: http %d: %s", ErrGraphQL, resp.StatusCode, string(respBody))
	}
	var envelope graphQLResponse
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("%w: %v", ErrGraphQL, err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("%w: %v", ErrGraphQL, envelope.Errors)
	}
	if err := json.Unmarshal(envelope.Data, dataTarget); err != nil {
		return fmt.Errorf("%w: %v", ErrGraphQL, err)
	}
	return nil
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

func missingConfig(config Config) []string {
	missing := make([]string, 0, 2)
	if strings.TrimSpace(config.GraphQLURL) == "" {
		missing = append(missing, "DAED_GRAPHQL_URL")
	}
	if strings.TrimSpace(config.Authorization) == "" {
		missing = append(missing, "DAED_AUTHORIZATION")
	}
	sort.Strings(missing)
	return missing
}

func findMatchingParen(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseDipEntries(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		result = appendUnique(result, seen, part)
	}
	return result
}
