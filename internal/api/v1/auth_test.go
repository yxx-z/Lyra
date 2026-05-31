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

func TestLogin_SuccessSetsAuthCookie(t *testing.T) {
	h := newTestAuthHandler()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "pass123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("want 1 cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != AuthCookieName {
		t.Fatalf("want cookie %q, got %q", AuthCookieName, cookie.Name)
	}
	if cookie.Value != "test-token" {
		t.Errorf("want cookie value test-token, got %q", cookie.Value)
	}
	if !cookie.HttpOnly {
		t.Error("want HttpOnly cookie")
	}
	if cookie.Path != "/" {
		t.Errorf("want cookie path /, got %q", cookie.Path)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("want SameSite=Lax, got %v", cookie.SameSite)
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

func TestLogout_ClearsAuthCookie(t *testing.T) {
	h := newTestAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	w := httptest.NewRecorder()
	h.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("want 1 cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != AuthCookieName {
		t.Fatalf("want cookie %q, got %q", AuthCookieName, cookie.Name)
	}
	if cookie.Value != "" {
		t.Errorf("want empty cookie value, got %q", cookie.Value)
	}
	if cookie.MaxAge >= 0 {
		t.Errorf("want expired cookie, got MaxAge=%d", cookie.MaxAge)
	}
}

func TestSession_SetsAuthCookie(t *testing.T) {
	h := newTestAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", nil)
	w := httptest.NewRecorder()
	h.Session(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("want 1 cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != AuthCookieName {
		t.Fatalf("want cookie %q, got %q", AuthCookieName, cookie.Name)
	}
	if cookie.Value != "test-token" {
		t.Errorf("want cookie value test-token, got %q", cookie.Value)
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
