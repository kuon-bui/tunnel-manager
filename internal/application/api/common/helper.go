package common

import (
	"errors"
	"net/http"
	"tunnelmanager/internal/model"

	"github.com/gin-gonic/gin"
)

func WriteGetErr(c *gin.Context, err error) {
	if errors.Is(err, model.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
}
