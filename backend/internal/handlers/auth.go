package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"live-polling/internal/services"
)

const invalidCredentialsMessage = "Email or password is incorrect."

type AuthHandler struct {
	auth   *services.AuthService
	secret string
}

func NewAuthHandler(auth *services.AuthService, secret string) *AuthHandler {
	return &AuthHandler{auth: auth, secret: secret}
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var input credentials
	if !decodeJSON(c, maxJSONBodyBytes, &input) {
		return
	}
	if input.Email == "" || input.Password == "" {
		writeError(c, http.StatusBadRequest, "Email and password are required.")
		return
	}
	user, err := h.auth.Register(c.Request.Context(), input.Email, input.Password)
	if err != nil {
		switch err {
		case services.ErrInvalidInput:
			writeError(c, http.StatusBadRequest, "Enter a valid email and a password with at least 8 characters.")
		case services.ErrEmailExists:
			writeError(c, http.StatusConflict, "An account with this email already exists.")
		default:
			writeError(c, http.StatusInternalServerError, "Unable to create your account right now.")
		}
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": user.Public()})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var input credentials
	if !decodeJSON(c, maxJSONBodyBytes, &input) {
		return
	}
	if input.Email == "" || input.Password == "" {
		writeError(c, http.StatusBadRequest, "Email and password are required as text.")
		return
	}
	user, err := h.auth.Login(c.Request.Context(), input.Email, input.Password)
	if err != nil {
		writeError(c, http.StatusUnauthorized, invalidCredentialsMessage)
		return
	}
	token, err := services.SignToken(user.ID.Hex(), h.secret)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Unable to sign in right now.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": user.Public()})
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID, ok := c.Get("userId")
	if !ok {
		writeError(c, http.StatusUnauthorized, "Authentication required.")
		return
	}
	user, err := h.auth.UserByID(c.Request.Context(), userID.(string))
	if err != nil {
		writeError(c, http.StatusUnauthorized, "Authentication required.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user.Public()})
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}
