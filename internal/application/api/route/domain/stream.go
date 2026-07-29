package domainroute

import (
	"io"
	"net/http"
	"time"

	"tunnelmanager/internal/model"
	domainrequest "tunnelmanager/internal/pkg/request/domain"

	"github.com/gin-gonic/gin"
)

var domainStreamHeartbeatInterval = 15 * time.Second

type domainListSnapshot struct {
	Items      []*model.Domain `json:"items"`
	NextCursor string          `json:"nextCursor"`
}

func (h *DomainHandler) streamDomains(c *gin.Context) {
	var req domainrequest.ListDomainRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates, cancel := h.domainService.Subscribe()
	defer cancel()
	items, nextCursor, err := h.domainService.ListDomains(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	writeSnapshot := true

	heartbeat := time.NewTicker(domainStreamHeartbeatInterval)
	defer heartbeat.Stop()
	c.Stream(func(w io.Writer) bool {
		if writeSnapshot {
			writeSnapshot = false
			c.SSEvent("domains", domainListSnapshot{Items: items, NextCursor: nextCursor})
			return true
		}
		select {
		case <-c.Request.Context().Done():
			return false
		case <-updates:
			items, nextCursor, err = h.domainService.ListDomains(c.Request.Context(), req)
			if err != nil {
				c.SSEvent("error", gin.H{"message": "stream unavailable"})
				return false
			}
			c.SSEvent("domains", domainListSnapshot{Items: items, NextCursor: nextCursor})
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": heartbeat\n\n")
		}
		return true
	})
}
