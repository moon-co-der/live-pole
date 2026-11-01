package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"live-polling/internal/services"
)

type PollHandler struct {
	polls *services.PollService
}

func NewPollHandler(polls *services.PollService) *PollHandler {
	return &PollHandler{polls: polls}
}

func (h *PollHandler) Create(c *gin.Context) {
	var input services.CreatePollInput
	if !decodeJSON(c, maxJSONBodyBytes, &input) {
		return
	}
	ownerID, ok := c.Get("userId")
	if !ok {
		writeError(c, http.StatusUnauthorized, "Authentication required.")
		return
	}
	poll, shareLink, err := h.polls.Create(c.Request.Context(), ownerID.(string), input)
	if err != nil {
		if errors.Is(err, services.ErrInvalidPoll) {
			writeError(c, http.StatusBadRequest, "Use a 3–200 character question and 2–6 unique options of up to 100 characters each. The end time must be in the future.")
			return
		}
		writeError(c, http.StatusInternalServerError, "Unable to create the poll right now.")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": poll.ID.Hex(), "shareLink": shareLink, "poll": poll})
}

func (h *PollHandler) List(c *gin.Context) {
	ownerID, ok := c.Get("userId")
	if !ok {
		writeError(c, http.StatusUnauthorized, "Authentication required.")
		return
	}
	page, err := queryInt(c, "page", 1)
	if err != nil {
		writeError(c, http.StatusBadRequest, "Page must be a positive integer.")
		return
	}
	limit, err := queryInt(c, "limit", 20)
	if err != nil {
		writeError(c, http.StatusBadRequest, "Limit must be between 1 and 100.")
		return
	}
	polls, total, err := h.polls.List(c.Request.Context(), ownerID.(string), services.PollListQuery{
		Status: c.Query("status"), Search: c.Query("q"), Sort: c.Query("sort"), Page: page, Limit: limit,
	})
	if err != nil {
		if errors.Is(err, services.ErrInvalidPoll) {
			writeError(c, http.StatusBadRequest, "Poll filters or pagination are invalid.")
			return
		}
		writeError(c, http.StatusInternalServerError, "Unable to load polls right now.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"polls": polls, "pagination": gin.H{"page": page, "limit": limit, "total": total}})
}

func (h *PollHandler) Get(c *gin.Context) {
	ownerID, ok := c.Get("userId")
	if !ok {
		writeError(c, http.StatusUnauthorized, "Authentication required.")
		return
	}
	poll, err := h.polls.GetOwned(c.Request.Context(), ownerID.(string), c.Param("id"))
	if errors.Is(err, services.ErrPollNotFound) {
		writeError(c, http.StatusNotFound, "Poll not found.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Unable to load the poll right now.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"poll": poll})
}

func (h *PollHandler) Close(c *gin.Context) {
	ownerID, ok := c.Get("userId")
	if !ok {
		writeError(c, http.StatusUnauthorized, "Authentication required.")
		return
	}
	poll, err := h.polls.Close(c.Request.Context(), ownerID.(string), c.Param("id"))
	if errors.Is(err, services.ErrPollNotFound) {
		writeError(c, http.StatusNotFound, "Poll not found.")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Unable to close the poll right now.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"poll": poll})
}

func (h *PollHandler) Delete(c *gin.Context) {
	ownerID, ok := c.Get("userId")
	if !ok {
		writeError(c, http.StatusUnauthorized, "Authentication required.")
		return
	}
	if err := h.polls.Delete(c.Request.Context(), ownerID.(string), c.Param("id")); errors.Is(err, services.ErrPollNotFound) {
		writeError(c, http.StatusNotFound, "Poll not found.")
		return
	} else if err != nil {
		writeError(c, http.StatusInternalServerError, "Unable to delete the poll right now.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Poll deleted."})
}

func queryInt(c *gin.Context, name string, fallback int64) (int64, error) {
	value := c.Query(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, errors.New("invalid integer")
	}
	if name == "limit" && parsed > 100 {
		return 0, errors.New("limit out of range")
	}
	return parsed, nil
}
