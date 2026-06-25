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
