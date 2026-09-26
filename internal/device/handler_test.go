package device

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func newTestHandler(repo Repository) http.Handler {
	mux := http.NewServeMux()
	NewHandler(NewService(repo, &fakeNotifier{}, discardLogger()), discardLogger()).Register(mux)
	return mux
}

// do sends a request through a handler backed by repo and returns the recorder.
func do(repo Repository, method, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	newTestHandler(repo).ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

// decodeError reads an errorResponse and fails if anything follows it.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var got errorResponse
	dec := json.NewDecoder(rec.Body)
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if dec.More() {
		t.Error("response has extra data after the error JSON")
	}
	return got.Error
}

func TestHandlerCreate(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
	}{
		{"valid", `{"name":"Phone X","brand":"Acme"}`, http.StatusCreated, ""},
		{"missing name", `{"brand":"Acme"}`, http.StatusBadRequest, "invalid input: name is required"},
		{"unknown state", `{"name":"Phone X","brand":"Acme","state":"broken"}`, http.StatusBadRequest, `invalid input: unknown state "broken"`},
		{"unknown field", `{"name":"Phone X","brand":"Acme","createdAt":"2026-01-01T00:00:00Z"}`, http.StatusBadRequest, `invalid input: malformed JSON body: json: unknown field "createdAt"`},
		{"malformed JSON", `{"name":`, http.StatusBadRequest, "invalid input: malformed JSON body: unexpected EOF"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			rec := do(repo, http.MethodPost, "/devices", tt.body)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if tt.wantError == "" {
				return
			}
			if got := decodeError(t, rec); got != tt.wantError {
				t.Errorf("error = %q, want %q", got, tt.wantError)
			}
		})
	}
}

func TestHandlerCreateResponse(t *testing.T) {
	repo := &fakeRepo{}
	rec := do(repo, http.MethodPost, "/devices", `{"name":" Phone X ", "brand":"Acme"}`)

	if loc := rec.Header().Get("Location"); loc != "/devices/test-1" {
		t.Errorf("Location = %q, want /devices/test-1", loc)
	}
	var got Device
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := Device{ID: "test-1", Name: "Phone X", Brand: "Acme", State: StateAvailable, CreatedAt: got.CreatedAt}
	if got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}
	if got.CreatedAt.IsZero() {
		t.Error("createdAt missing from response")
	}
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	repo := &fakeRepo{}
	rec := do(repo, http.MethodDelete, "/devices", "")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandlerGet(t *testing.T) {
	const knownID = "7f1c2b9e-3a4d-4e5f-8a6b-1c2d3e4f5a6b"
	const unknownID = "00000000-0000-4000-8000-000000000000"
	known := Device{ID: knownID, Name: "Phone X", Brand: "Acme", State: StateAvailable,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

	tests := []struct {
		name       string
		id         string
		wantStatus int
		wantError  string
	}{
		{"known id", knownID, http.StatusOK, ""},
		{"unknown id", unknownID, http.StatusNotFound, "device not found"},
		{"malformed id", "abc", http.StatusBadRequest, `invalid input: invalid id "abc"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{devices: map[string]Device{knownID: known}}
			rec := do(repo, http.MethodGet, "/devices/"+tt.id, "")

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantError != "" {
				if got := decodeError(t, rec); got != tt.wantError {
					t.Errorf("error = %q, want %q", got, tt.wantError)
				}
				return
			}

			var got Device
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got != known {
				t.Errorf("body = %+v, want %+v", got, known)
			}
		})
	}
}

func TestHandlerList(t *testing.T) {
	a := Device{ID: "a", Name: "Phone A", Brand: "Acme", State: StateAvailable}
	b := Device{ID: "b", Name: "Phone B", Brand: "Acme", State: StateInUse}

	tests := []struct {
		name       string
		target     string
		listed     []Device
		wantStatus int
		wantFilter Filter
		wantBody   string
	}{
		{"no filter", "/devices", []Device{a, b}, http.StatusOK, Filter{}, ""},
		{"brand and state", "/devices?brand=Acme&state=in-use", []Device{b}, http.StatusOK, Filter{Brand: "Acme", State: StateInUse}, ""},
		{"empty result is an array", "/devices?brand=Nope", nil, http.StatusOK, Filter{Brand: "Nope"}, "[]\n"},
		{"unknown state", "/devices?state=broken", nil, http.StatusBadRequest, Filter{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{listed: tt.listed}
			rec := do(repo, http.MethodGet, tt.target, "")

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus != http.StatusOK {
				if len(repo.filters) != 0 {
					t.Errorf("repo.List called with %+v, want no call", repo.filters)
				}
				return
			}
			if len(repo.filters) != 1 || repo.filters[0] != tt.wantFilter {
				t.Errorf("repo.List filters = %+v, want [%+v]", repo.filters, tt.wantFilter)
			}
			if tt.wantBody != "" {
				if got := rec.Body.String(); got != tt.wantBody {
					t.Errorf("body = %q, want %q", got, tt.wantBody)
				}
				return
			}

			var got []Device
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if !slices.Equal(got, tt.listed) {
				t.Errorf("body = %+v, want %+v", got, tt.listed)
			}
		})
	}
}

func TestHandlerUpdate(t *testing.T) {
	const availableID = "7f1c2b9e-3a4d-4e5f-8a6b-1c2d3e4f5a6b"
	const inUseID = "2b8e4c1d-6f3a-4b7e-9c2d-8e1f3a5b7c9d"
	const unknownID = "00000000-0000-4000-8000-000000000000"
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	available := Device{ID: availableID, Name: "Phone X", Brand: "Acme", State: StateAvailable, CreatedAt: created}
	inUse := Device{ID: inUseID, Name: "Phone Y", Brand: "Acme", State: StateInUse, CreatedAt: created}

	tests := []struct {
		name        string
		method      string
		id          string
		body        string
		wantStatus  int
		wantError   string
		want        Device // checked on 200
		wantRepoHit bool
	}{
		{
			name: "PUT replaces every field", method: http.MethodPut, id: availableID,
			body:       `{"name":"Phone Z","brand":"Globex","state":"inactive"}`,
			wantStatus: http.StatusOK, wantRepoHit: true,
			want: Device{ID: availableID, Name: "Phone Z", Brand: "Globex", State: StateInactive, CreatedAt: created},
		},
		{
			name: "PUT with a missing field", method: http.MethodPut, id: availableID,
			body:       `{"name":"Phone Z","brand":"Globex"}`,
			wantStatus: http.StatusBadRequest, wantError: "invalid input: name, brand and state are required",
		},
		{
			name: "PUT with createdAt", method: http.MethodPut, id: availableID,
			body:       `{"name":"Phone Z","brand":"Globex","state":"inactive","createdAt":"2020-01-01T00:00:00Z"}`,
			wantStatus: http.StatusBadRequest, wantError: `invalid input: malformed JSON body: json: unknown field "createdAt"`,
		},
		{
			name: "PATCH changes only the given field", method: http.MethodPatch, id: availableID,
			body:       `{"state":"in-use"}`,
			wantStatus: http.StatusOK, wantRepoHit: true,
			want: Device{ID: availableID, Name: "Phone X", Brand: "Acme", State: StateInUse, CreatedAt: created},
		},
		{
			name: "PATCH with an empty object changes nothing", method: http.MethodPatch, id: availableID,
			body:       `{}`,
			wantStatus: http.StatusOK, wantRepoHit: true, want: available,
		},
		{
			name: "PATCH name of an in-use device", method: http.MethodPatch, id: inUseID,
			body:       `{"name":"Renamed"}`,
			wantStatus: http.StatusConflict, wantError: "device is in use: cannot change name", wantRepoHit: true,
		},
		{
			name: "PATCH unknown id", method: http.MethodPatch, id: unknownID,
			body:       `{"state":"inactive"}`,
			wantStatus: http.StatusNotFound, wantError: "device not found", wantRepoHit: true,
		},
		{
			name: "PATCH malformed id", method: http.MethodPatch, id: "abc",
			body:       `{"state":"inactive"}`,
			wantStatus: http.StatusBadRequest, wantError: `invalid input: invalid id "abc"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{devices: map[string]Device{availableID: available, inUseID: inUse}}
			rec := do(repo, tt.method, "/devices/"+tt.id, tt.body)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if hit := len(repo.updateIDs) > 0; hit != tt.wantRepoHit {
				t.Errorf("repo.Update called = %v, want %v", hit, tt.wantRepoHit)
			}
			if tt.wantError != "" {
				if got := decodeError(t, rec); got != tt.wantError {
					t.Errorf("error = %q, want %q", got, tt.wantError)
				}
				return
			}

			var got Device
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got != tt.want {
				t.Errorf("body = %+v, want %+v", got, tt.want)
			}
		})
	}
}
