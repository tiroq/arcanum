package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/tiroq/arcanum/internal/health"
)

const testAdminToken = "test-secret-token"

func newTestRouter() http.Handler {
	registry := prometheus.NewRegistry()
	handlers := &Handlers{}
	rc := &health.ReadinessChecker{}
	return NewRouter(handlers, registry, rc, testAdminToken)
}

func TestHealthEndpoint(t *testing.T) {
	router := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	router := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	router := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	req.Header.Set("X-Admin-Token", "wrong-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	// This will hit the handler which needs a DB, so we expect 500 or similar (not 401/403).
	router := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	req.Header.Set("X-Admin-Token", testAdminToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should not be 401 or 403 (auth passed, handler may fail without DB)
	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		t.Errorf("expected auth to pass, got %d", w.Code)
	}
}
