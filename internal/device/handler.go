package device

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// Handler exposes the device Service over HTTP as JSON.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// Register adds the device routes to mux.
//
// TODO: the API has no authentication, authorization or rate limiting; add
// them as middleware before exposing it beyond a trusted network.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /devices", h.create)
	mux.HandleFunc("GET /devices", h.list)
	mux.HandleFunc("GET /devices/{id}", h.get)
	mux.HandleFunc("PUT /devices/{id}", h.replace)
	mux.HandleFunc("PATCH /devices/{id}", h.patch)
	mux.HandleFunc("DELETE /devices/{id}", h.remove)
}

type createRequest struct {
	Name  string `json:"name"`
	Brand string `json:"brand"`
	State State  `json:"state"`
}

// updateRequest uses pointers so a missing field (nil) can be told apart
// from an empty one ("").
type updateRequest struct {
	Name  *string `json:"name"`
	Brand *string `json:"brand"`
	State *State  `json:"state"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}

	d, err := h.service.Create(r.Context(), CreateInput(req))
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	w.Header().Set("Location", "/devices/"+d.ID)
	writeJSON(w, http.StatusCreated, d)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	d, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := Filter{Brand: q.Get("brand"), State: State(q.Get("state"))}

	devices, err := h.service.List(r.Context(), filter)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	if devices == nil {
		devices = []Device{} // always a JSON array, not null
	}

	writeJSON(w, http.StatusOK, devices)
}

// replace handles PUT: the body is the full new representation, so every
// field is required.
//
// TODO: two clients can PUT from the same stale read and the last one wins.
// ETag + If-Match (412 on mismatch) would prevent lost updates.
func (h *Handler) replace(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}

	if req.Name == nil || req.Brand == nil || req.State == nil {
		h.writeError(w, r, fmt.Errorf("%w: name, brand and state are required", ErrInvalidInput))
		return
	}

	h.update(w, r, req)
}

// patch handles PATCH: only the fields present in the body change.
func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}

	h.update(w, r, req)
}

// update applies req to the device in the path; PUT and PATCH both end here.
func (h *Handler) update(w http.ResponseWriter, r *http.Request, req updateRequest) {
	d, err := h.service.Update(r.Context(), r.PathValue("id"), UpdateInput(req))
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, d)
}

// remove handles DELETE: returns no content and no body as device no longer exists
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.writeError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TODO: a body over the limit is reported as 400; errors.As on
// *http.MaxBytesError could return 413 Request Entity Too Large instead.
const maxBodyBytes = 1 << 20 // 1 MiB

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: malformed JSON body: %w", ErrInvalidInput, err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: ErrNotFound.Error()})
	case errors.Is(err, ErrInUse):
		writeJSON(w, http.StatusConflict, errorResponse{Error: err.Error()})
	default:
		h.logger.ErrorContext(r.Context(), "request failed",
			"method", r.Method, "path", r.URL.Path, "err", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}
