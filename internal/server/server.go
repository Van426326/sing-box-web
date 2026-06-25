package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"

	"kt-proxy/internal/configmgr"
	"kt-proxy/internal/daedsync"
)

type ConfigService interface {
	Load(ctx context.Context) (*configmgr.LoadResult, error)
	Save(ctx context.Context, raw json.RawMessage) (*configmgr.SaveResult, error)
}

type DaedSyncService interface {
	Sync(ctx context.Context) (*daedsync.Result, error)
}

func New(service ConfigService, staticFS fs.FS, daed DaedSyncService) http.Handler {
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
	mux.HandleFunc("POST /api/daed/sync-route-rules", func(w http.ResponseWriter, r *http.Request) {
		if daed == nil {
			writeError(w, http.StatusBadRequest, daedsync.ErrMissingConfig)
			return
		}
		result, err := daed.Sync(r.Context())
		if err != nil {
			writeJSON(w, daedStatus(err), map[string]any{
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

func daedStatus(err error) int {
	if errors.Is(err, daedsync.ErrMissingConfig) {
		return http.StatusBadRequest
	}
	if errors.Is(err, daedsync.ErrNoSelectedRouting) ||
		errors.Is(err, daedsync.ErrMarkerNotFound) ||
		errors.Is(err, daedsync.ErrTargetBlockNotFound) {
		return http.StatusUnprocessableEntity
	}
	if errors.Is(err, daedsync.ErrGraphQL) {
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
