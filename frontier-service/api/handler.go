package api

import (
	"encoding/json"
	"github.com/crawler-monorepo/frontier-service/service"
	"github.com/go-chi/chi/v5"
	"net/http"
)

type Handler struct {
	frontier *service.FrontierService
}

func NewHandler(f *service.FrontierService) *Handler {
	return &Handler{frontier: f}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v1/seeds", h.addSeeds)
	r.Get("/api/v1/status", h.status)
}

type SeedRequest struct {
	URLs []string `json:"urls"`
}

func (h *Handler) addSeeds(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 1MB to prevent abuse
	const maxRequestBody = 1 * 1024 * 1024 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req SeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.URLs) == 0 {
		http.Error(w, "urls list cannot be empty", http.StatusBadRequest)
		return
	}

	added, err := h.frontier.AddSeeds(r.Context(), req.URLs)
	if err != nil {
		http.Error(w, "Failed to process seeds", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"submitted": len(req.URLs),
		"added":     added,
	})
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"service": "frontier",
	})
}
