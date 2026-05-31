// internal/api/v1/auth_test.go
package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/config"
)

func newTestAuthHandler() *AuthHandler {
	return NewAuthHandler(&config.Config{
		Auth: config.AuthConfig{
			Username: "admin",
			Password: "pass123",
			Token:    "test-token",
		},
	})
}

func TestLogin_Success(t *testing.T) {
	h := newTestAuthHandler()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "pass123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["token"] != "test-token" {
		t.Errorf("want token=test-token, got %q", resp["token"])
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h := newTestAuthHandler()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestLogin_BadJSON(t *testing.T) {
	h := newTestAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestLogin_EmptyConfiguredPasswordRejected(t *testing.T) {
	h := NewAuthHandler(&config.Config{
		Auth: config.AuthConfig{
			Username: "admin",
			Password: "",
			Token:    "test-token",
		},
	})
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}
