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
	"kt-proxy/internal/daedsync"
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

type fakeDaedSyncService struct {
	result *daedsync.Result
	err    error
	calls  int
}

func (f *fakeDaedSyncService) Sync(ctx context.Context) (*daedsync.Result, error) {
	f.calls++
	return f.result, f.err
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
	handler := New(service, testStaticFS(), nil)

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
	handler := New(service, testStaticFS(), nil)

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
	handler := New(service, testStaticFS(), nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/config/save", strings.NewReader(`{"config":{"x":1}}`))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestStaticIndexServed(t *testing.T) {
	handler := New(&fakeConfigService{}, testStaticFS(), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), "kt-proxy") {
		t.Fatalf("status=%d body=%s", rec.Code, string(body))
	}
}

func TestLoadErrorReturns500(t *testing.T) {
	service := &fakeConfigService{loadErr: errors.New("boom")}
	handler := New(service, testStaticFS(), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestDaedSyncReturnsResult(t *testing.T) {
	daed := &fakeDaedSyncService{result: &daedsync.Result{
		RoutingID:   "r1",
		RoutingName: "default",
		Changed:     true,
		Added:       []string{"10.0.0.0/24"},
		Message:     "已同步 1 个 IP 到 Daed",
	}}
	handler := New(&fakeConfigService{}, testStaticFS(), daed)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/daed/sync-route-rules", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if daed.calls != 1 {
		t.Fatalf("calls = %d, want 1", daed.calls)
	}
	if !strings.Contains(rec.Body.String(), `"routingName":"default"`) {
		t.Fatalf("body missing result: %s", rec.Body.String())
	}
}

func TestDaedSyncMapsErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "missing config", err: daedsync.ErrMissingConfig, want: http.StatusBadRequest},
		{name: "missing marker", err: daedsync.ErrMarkerNotFound, want: http.StatusUnprocessableEntity},
		{name: "graphql", err: daedsync.ErrGraphQL, want: http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := New(&fakeConfigService{}, testStaticFS(), &fakeDaedSyncService{err: tt.err})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/daed/sync-route-rules", nil)
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestDaedSyncWithoutServiceReturns400(t *testing.T) {
	handler := New(&fakeConfigService{}, testStaticFS(), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/daed/sync-route-rules", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func testStaticFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {Data: []byte(`<html><body>kt-proxy</body></html>`)},
	}
}
