package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"live-polling/internal/services"
)

const voterCookieName = "voterId"

var voterUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var errInvalidVoterID = errors.New("X-Voter-Id must be a UUID")

type PublicPollHandler struct {
	polls        *services.PublicPollService
	cookieSecure bool
}

func NewPublicPollHandler(polls *services.PublicPollService, cookieSecure bool) *PublicPollHandler {
	return &PublicPollHandler{polls: polls, cookieSecure: cookieSecure}
}

func (h *PublicPollHandler) Get(c *gin.Context) {
	voterID, err := getOrCreateVoterID(c, h.cookieSecure)
	if err != nil {
		if errors.Is(err, errInvalidVoterID) {
			writeError(c, http.StatusBadRequest, "A valid voter identifier is required.")
			return
		}
		writeError(c, http.StatusInternalServerError, "Unable to initialize this voting session.")
		return
	}
	poll, err := h.polls.Get(c.Request.Context(), c.Param("slug"), voterID, c.GetHeader("Referer"))
	if errors.Is(err, services.ErrPublicPollNotFound) {
		writeError(c, http.StatusNotFound, "Poll not found.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Unable to load this poll right now.")
		return
	}
	c.JSON(http.StatusOK, poll)
}

func (h *PublicPollHandler) Vote(c *gin.Context) {
	voterID, err := getOrCreateVoterID(c, h.cookieSecure)
	if err != nil {
		if errors.Is(err, errInvalidVoterID) {
			writeError(c, http.StatusBadRequest, "A valid voter identifier is required.")
			return
		}
		writeError(c, http.StatusInternalServerError, "Unable to initialize this voting session.")
		return
	}
	var input struct {
		OptionID string `json:"optionId"`
	}
	if !decodeJSON(c, 1024, &input) || strings.TrimSpace(input.OptionID) == "" {
		if c.Writer.Written() {
			return
		}
		writeError(c, http.StatusBadRequest, "Choose a valid poll option.")
		return
	}
	input.OptionID = strings.TrimSpace(input.OptionID)
	results, err := h.polls.Vote(c.Request.Context(), c.Param("slug"), voterID, input.OptionID, c.GetHeader("User-Agent"), c.GetHeader("Referer"))
	switch {
	case errors.Is(err, services.ErrPublicPollNotFound):
		writeError(c, http.StatusNotFound, "Poll not found.")
	case errors.Is(err, services.ErrInvalidVote):
		writeError(c, http.StatusBadRequest, "Choose a valid poll option.")
	case errors.Is(err, services.ErrClosedPoll):
		writeError(c, http.StatusBadRequest, "This poll is closed.")
	case errors.Is(err, services.ErrAlreadyVoted):
		writeError(c, http.StatusConflict, "You already voted.")
	case err != nil:
		writeError(c, http.StatusInternalServerError, "Unable to submit your vote right now.")
	default:
		c.JSON(http.StatusOK, gin.H{"results": results})
	}
}

func getOrCreateVoterID(c *gin.Context, cookieSecure bool) (string, error) {
	if value := strings.TrimSpace(c.GetHeader("X-Voter-Id")); value != "" {
		if !voterUUIDPattern.MatchString(value) {
			return "", errInvalidVoterID
		}
		return strings.ToLower(value), nil
	}
	if value, err := c.Cookie(voterCookieName); err == nil && validVoterID(value) {
		return value, nil
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate voter identifier: %w", err)
	}
	voterID := base64.RawURLEncoding.EncodeToString(bytes[:])
	sameSite := http.SameSiteLaxMode
	if cookieSecure {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: voterCookieName, Value: voterID, Path: "/", MaxAge: 365 * 24 * 60 * 60,
		Expires: time.Now().Add(365 * 24 * time.Hour), HttpOnly: true, Secure: cookieSecure,
		SameSite: sameSite,
	})
	return voterID, nil
}

func validVoterID(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}
