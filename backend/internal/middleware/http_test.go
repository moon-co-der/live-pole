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
