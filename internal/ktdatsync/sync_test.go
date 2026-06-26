package ktdatsync

import (
	"context"
	"encoding/base64"
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
	assertStrings(t, got, []string{"10.0.0.0/24", "192.168.1.1/32"})
}

func TestRenderKTTextWritesOneCIDRPerLine(t *testing.T) {
	got := RenderKTText([]string{"10.0.0.0/24", "192.168.1.1/32"})
	want := "10.0.0.0/24\n192.168.1.1/32\n"
	if got != want {
		t.Fatalf("RenderKTText = %q, want %q", got, want)
	}
	if empty := RenderKTText(nil); empty != "" {
		t.Fatalf("RenderKTText(nil) = %q, want empty string", empty)
	}
}

func TestSyncRequiresGitHubConfig(t *testing.T) {
	service := New(Config{}, fakeLoader{}, http.DefaultClient)
	_, err := service.Sync(context.Background())
	if !errors.Is(err, ErrMissingConfig) {
		t.Fatalf("Sync error = %v, want ErrMissingConfig", err)
	}
}

func TestSyncRejectsInvalidRepo(t *testing.T) {
	service := New(Config{Repo: "bad", Token: "token"}, fakeLoader{}, http.DefaultClient)
	_, err := service.Sync(context.Background())
	if !errors.Is(err, ErrInvalidRepo) {
		t.Fatalf("Sync error = %v, want ErrInvalidRepo", err)
	}
}

func TestSyncNoopsWhenRemoteContentMatches(t *testing.T) {
	server := newGitHubServer(t, githubScenario{
		existingContent: "10.0.0.0/24\n",
		existingSHA:     "abc123",
	})
	defer server.Close()

	service := newTestService(server, `{"route":{"rules":[{"ip_cidr":"10.0.0.0/24"}]}}`)
	result, err := service.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if result.Changed {
		t.Fatalf("Changed = true, want false")
	}
	if result.CIDRCount != 1 || result.Target != "Van426326/kt-dat:main:kt.txt" {
		t.Fatalf("result mismatch: %+v", result)
	}
	if server.putCalls != 0 {
		t.Fatalf("putCalls = %d, want 0", server.putCalls)
	}
}

func TestSyncUpdatesExistingFileWhenContentDiffers(t *testing.T) {
	server := newGitHubServer(t, githubScenario{
		existingContent: "10.0.0.0/24\n",
		existingSHA:     "abc123",
		commitSHA:       "commit456",
		commitURL:       "https://github.com/Van426326/kt-dat/commit/commit456",
	})
	defer server.Close()

	service := newTestService(server, `{"route":{"rules":[{"ip_cidr":["10.0.0.0/24","192.168.1.1/32"]}]}}`)
	result, err := service.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if !result.Changed || result.CIDRCount != 2 || result.CommitSHA != "commit456" || result.CommitURL == "" {
		t.Fatalf("result mismatch: %+v", result)
	}
	if server.putCalls != 1 {
		t.Fatalf("putCalls = %d, want 1", server.putCalls)
	}
	if server.lastPut.SHA != "abc123" {
		t.Fatalf("put sha = %q, want existing sha", server.lastPut.SHA)
	}
	if decoded := mustDecodeBase64(t, server.lastPut.Content); decoded != "10.0.0.0/24\n192.168.1.1/32\n" {
		t.Fatalf("put content = %q", decoded)
	}
}

func TestSyncCreatesFileWhenMissing(t *testing.T) {
	server := newGitHubServer(t, githubScenario{
		missing:   true,
		commitSHA: "commit789",
		commitURL: "https://github.com/Van426326/kt-dat/commit/commit789",
	})
	defer server.Close()

	service := newTestService(server, `{"route":{"rules":[{"ip_cidr":"10.0.0.0/24"}]}}`)
	result, err := service.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if !result.Changed || result.CommitSHA != "commit789" {
		t.Fatalf("result mismatch: %+v", result)
	}
	if server.lastPut.SHA != "" {
		t.Fatalf("put sha = %q, want empty sha for create", server.lastPut.SHA)
	}
}

func TestSyncMapsGitHubConflict(t *testing.T) {
	server := newGitHubServer(t, githubScenario{
		existingContent: "10.0.0.0/24\n",
		existingSHA:     "abc123",
		putStatus:       http.StatusConflict,
	})
	defer server.Close()

	service := newTestService(server, `{"route":{"rules":[{"ip_cidr":["10.0.0.0/24","192.168.1.1/32"]}]}}`)
	_, err := service.Sync(context.Background())
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Sync error = %v, want ErrConflict", err)
	}
}

type githubScenario struct {
	existingContent string
	existingSHA     string
	missing         bool
	putStatus       int
	commitSHA       string
	commitURL       string
}

type testGitHubServer struct {
	*httptest.Server
	putCalls int
	lastPut  putContentRequest
}

func newGitHubServer(t *testing.T, scenario githubScenario) *testGitHubServer {
	t.Helper()
	state := &testGitHubServer{}
	state.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/repos/Van426326/kt-dat/contents/kt.txt" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("ref") != "main" {
				t.Fatalf("ref = %q, want main", r.URL.Query().Get("ref"))
			}
			if scenario.missing {
				writeGitHubJSON(t, w, http.StatusNotFound, map[string]any{"message": "Not Found"})
				return
			}
			writeGitHubJSON(t, w, http.StatusOK, map[string]any{
				"sha":      scenario.existingSHA,
				"content":  base64.StdEncoding.EncodeToString([]byte(scenario.existingContent)),
				"encoding": "base64",
			})
		case http.MethodPut:
			state.putCalls++
			if err := json.NewDecoder(r.Body).Decode(&state.lastPut); err != nil {
				t.Fatalf("decode put: %v", err)
			}
			if state.lastPut.Message != "chore: update kt cidr list" {
				t.Fatalf("message = %q", state.lastPut.Message)
			}
			if state.lastPut.Branch != "main" {
				t.Fatalf("branch = %q", state.lastPut.Branch)
			}
			status := scenario.putStatus
			if status == 0 {
				status = http.StatusOK
			}
			if status >= 300 {
				writeGitHubJSON(t, w, status, map[string]any{"message": "conflict"})
				return
			}
			writeGitHubJSON(t, w, status, map[string]any{
				"commit": map[string]any{
					"sha":      scenario.commitSHA,
					"html_url": scenario.commitURL,
				},
			})
		default:
			t.Fatalf("method = %s", r.Method)
		}
	}))
	return state
}

func newTestService(server *testGitHubServer, rawConfig string) *Service {
	loader := fakeLoader{result: &configmgr.LoadResult{
		Config:   json.RawMessage(rawConfig),
		LoadedAt: time.Now(),
	}}
	return New(Config{
		Repo:    "Van426326/kt-dat",
		Branch:  "main",
		Path:    "kt.txt",
		Token:   "token",
		APIBase: server.URL,
	}, loader, server.Client())
}

func mustDecodeBase64(t *testing.T, value string) string {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	return string(decoded)
}

func writeGitHubJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func assertStrings(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got=%v", len(got), len(want), got)
	}
	for i := range want {
		if strings.TrimSpace(got[i]) != want[i] {
			t.Fatalf("got[%d] = %q, want %q; got=%v", i, got[i], want[i], got)
		}
	}
}
