package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestJSONRecoveryHidesPanicDetailsFromClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(JSONRecovery())
	router.GET("/panic", func(*gin.Context) { panic("private diagnostic") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want=%d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), "private diagnostic") || !strings.Contains(response.Body.String(), "Internal server error") {
		t.Fatalf("unexpected recovery response: %s", response.Body.String())
	}
}

func TestCORSPreflightAllowsConfiguredOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	origin := "https://frontend-tcln-git-main-moon-co-ders-projects.vercel.app"
	router := gin.New()
	router.Use(CORS("https://app.example.invalid," + origin))
	router.OPTIONS("/api/auth/register", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodOptions, "/api/auth/register", nil)
	request.Header.Set("Origin", origin)
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d want=%d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("allow-origin=%q want=%q", got, origin)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials=%q want=true", got)
	}
}

func TestCORSPreflightDoesNotAllowUnconfiguredOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORS("https://app.example.invalid"))
	router.OPTIONS("/api/auth/register", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodOptions, "/api/auth/register", nil)
	request.Header.Set("Origin", "https://untrusted.example.invalid")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d want=%d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unconfigured origin received allow-origin=%q", got)
	}
}
