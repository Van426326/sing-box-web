package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"

	"kt-proxy/internal/configmgr"
	"kt-proxy/internal/ktdatsync"
)

type ConfigService interface {
	Load(ctx context.Context) (*configmgr.LoadResult, error)
	Save(ctx context.Context, raw json.RawMessage) (*configmgr.SaveResult, error)
}

type KTDatSyncService interface {
	Sync(ctx context.Context) (*ktdatsync.Result, error)
}

func New(service ConfigService, staticFS fs.FS, ktdat KTDatSyncService) http.Handler {
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
	mux.HandleFunc("POST /api/ktdat/sync", func(w http.ResponseWriter, r *http.Request) {
		if ktdat == nil {
			writeError(w, http.StatusBadRequest, ktdatsync.ErrMissingConfig)
			return
		}
		result, err := ktdat.Sync(r.Context())
		if err != nil {
			writeJSON(w, ktDatStatus(err), map[string]any{
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

func ktDatStatus(err error) int {
	if errors.Is(err, ktdatsync.ErrMissingConfig) || errors.Is(err, ktdatsync.ErrInvalidRepo) {
		return http.StatusBadRequest
	}
	if errors.Is(err, ktdatsync.ErrConflict) {
		return http.StatusConflict
	}
	if errors.Is(err, ktdatsync.ErrGitHub) {
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
