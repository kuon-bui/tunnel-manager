package domainroute

import (
	"encoding/json"
	"net/http"
	"time"

	"tunnelmanager/internal/application/api/common"

	"github.com/gin-gonic/gin"
)

var (
	domainDetailMetricsInterval   = 4 * time.Second
	domainDetailHeartbeatInterval = 15 * time.Second
)

type logsEvent struct {
	Items []string `json:"items"`
}

type metricsEvent struct {
	Text string `json:"text"`
}

type streamErrorEvent struct {
	Message string `json:"message"`
}

func writeDetailSSE(c *gin.Context, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if event != "" {
		if _, err := c.Writer.WriteString("event: " + event + "\n"); err != nil {
			return err
		}
	}
	if _, err := c.Writer.WriteString("data: " + string(data) + "\n\n"); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func (h *DomainHandler) streamDomainDetail(c *gin.Context) {
	ctx := c.Request.Context()
	lines, updates, cancel, err := h.domainService.SubscribeLogs(ctx, c.Param("id"))
	if err != nil {
		common.WriteGetErr(c, err)
		return
	}
	defer cancel()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	if err := writeDetailSSE(c, "logs", logsEvent{Items: lines}); err != nil {
		return
	}
	if !h.writeMetrics(c) {
		return
	}

	metricsTicker := time.NewTicker(domainDetailMetricsInterval)
	heartbeatTicker := time.NewTicker(domainDetailHeartbeatInterval)
	defer metricsTicker.Stop()
	defer heartbeatTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-updates:
			if !ok {
				return
			}
			lines, err := h.domainService.Logs(ctx, c.Param("id"))
			if err != nil {
				_ = writeDetailSSE(c, "error", streamErrorEvent{Message: "stream unavailable"})
				return
			}
			if err := writeDetailSSE(c, "logs", logsEvent{Items: lines}); err != nil {
				return
			}
		case <-metricsTicker.C:
			if !h.writeMetrics(c) {
				return
			}
		case <-heartbeatTicker.C:
			if _, err := c.Writer.WriteString(": heartbeat\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}

func (h *DomainHandler) writeMetrics(c *gin.Context) bool {
	text, err := h.domainService.Metrics(c.Request.Context(), c.Param("id"))
	if err != nil {
		return writeDetailSSE(c, "metrics-error", streamErrorEvent{Message: "metrics unavailable"}) == nil
	}
	return writeDetailSSE(c, "metrics", metricsEvent{Text: text}) == nil
}
