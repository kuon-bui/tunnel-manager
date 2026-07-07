package http

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"tunnelmanager/internal/application/domain"
	"tunnelmanager/internal/model"
)

type DomainResponse struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	OriginURL    string    `json:"origin_url"`
	Status       string    `json:"status"`
	MetricsPort  int       `json:"metrics_port"`
	PID          int       `json:"pid"`
	RestartCount int       `json:"restart_count"`
	LastError    string    `json:"last_error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func toResponse(domain *model.Domain) DomainResponse {
	return DomainResponse{
		ID:           domain.ID,
		Hostname:     domain.Hostname,
		OriginURL:    domain.OriginURL,
		Status:       string(domain.Status),
		MetricsPort:  domain.MetricsPort,
		PID:          domain.PID,
		RestartCount: domain.RestartCount,
		LastError:    domain.LastError,
		CreatedAt:    domain.CreatedAt,
		UpdatedAt:    domain.UpdatedAt,
	}
}

type handlers struct {
	svc domain.DomainService
}

func writeGetErr(c *gin.Context, err error) {
	if errors.Is(err, model.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
}

type createDomainRequest struct {
	Hostname  string `json:"hostname" binding:"required"`
	OriginURL string `json:"origin_url" binding:"required"`
}

func (h *handlers) createDomain(c *gin.Context) {
	var req createDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	domain, err := h.svc.CreateDomain(c.Request.Context(), req.Hostname, req.OriginURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, toResponse(domain))
}

func (h *handlers) listDomains(c *gin.Context) {
	domains, err := h.svc.ListDomains(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := make([]DomainResponse, 0, len(domains))
	for i := range domains {
		resp = append(resp, toResponse(&domains[i]))
	}
	c.JSON(http.StatusOK, resp)
}

func (h *handlers) getDomain(c *gin.Context) {
	domain, err := h.svc.GetDomain(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeGetErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toResponse(domain))
}

type updateDomainRequest struct {
	OriginURL string `json:"origin_url" binding:"required"`
}

func (h *handlers) updateDomain(c *gin.Context) {
	var req updateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	domain, err := h.svc.UpdateOrigin(c.Request.Context(), c.Param("id"), req.OriginURL)
	if err != nil {
		writeGetErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toResponse(domain))
}

func (h *handlers) deleteDomain(c *gin.Context) {
	if err := h.svc.DeleteDomain(c.Request.Context(), c.Param("id")); err != nil {
		writeGetErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) stopDomain(c *gin.Context) {
	if err := h.svc.StopDomain(c.Request.Context(), c.Param("id")); err != nil {
		writeGetErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) restartDomain(c *gin.Context) {
	if err := h.svc.RestartDomain(c.Request.Context(), c.Param("id")); err != nil {
		writeGetErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) getLogs(c *gin.Context) {
	lines, err := h.svc.Logs(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeGetErr(c, err)
		return
	}
	c.JSON(http.StatusOK, lines)
}

func (h *handlers) getMetrics(c *gin.Context) {
	if err := h.svc.ProxyMetrics(c.Request.Context(), c.Param("id"), c.Writer); err != nil {
		writeGetErr(c, err)
		return
	}
}
