package device

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestHandler(repo Repository) http.Handler {
	mux := http.NewServeMux()
	NewHandler(newTestService(repo), discardLogger()).Register(mux)
	return mux
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
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/devices", strings.NewReader(tt.body))
			newTestHandler(&fakeRepo{}).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if tt.wantError == "" {
				return
			}
			var got errorResponse
			dec := json.NewDecoder(rec.Body)
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if dec.More() {
				t.Error("response has extra data after the error JSON")
			}
			if got.Error != tt.wantError {
				t.Errorf("error = %q, want %q", got.Error, tt.wantError)
			}
		})
	}
}

func TestHandlerCreateResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/devices", strings.NewReader(`{"name":" Phone X ","brand":"Acme"}`))
	newTestHandler(&fakeRepo{}).ServeHTTP(rec, req)

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
	rec := httptest.NewRecorder()
	newTestHandler(&fakeRepo{}).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/devices", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
