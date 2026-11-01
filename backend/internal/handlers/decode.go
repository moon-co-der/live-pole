package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

const maxJSONBodyBytes = 16 * 1024

func decodeJSON(c *gin.Context, limit int64, destination any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(c, http.StatusRequestEntityTooLarge, "Request body is too large.")
		} else {
			writeError(c, http.StatusBadRequest, "Request body is invalid.")
		}
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(c, http.StatusRequestEntityTooLarge, "Request body is too large.")
		} else {
			writeError(c, http.StatusBadRequest, "Request body must contain one JSON value.")
		}
		return false
	}
	return true
}
