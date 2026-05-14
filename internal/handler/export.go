package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pdai/pdai/internal/model"
	"github.com/pdai/pdai/internal/service"
)

// ExportHandler manages config import/export endpoints
type ExportHandler struct {
	svc *service.HostService
}

// NewExportHandler creates a new ExportHandler
func NewExportHandler(svc *service.HostService) *ExportHandler {
	return &ExportHandler{svc: svc}
}

// Export returns all hosts as a JSON download
func (h *ExportHandler) Export(c *gin.Context) {
	data, err := h.svc.ExportAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Disposition", "attachment; filename=Pdai-export.json")
	c.JSON(http.StatusOK, data)
}

// Import replaces all hosts from an uploaded JSON file
func (h *ExportHandler) Import(c *gin.Context) {
	var data model.ExportData
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid import data: " + err.Error()})
		return
	}

	if err := h.svc.ImportAll(&data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Configuration imported successfully",
		"hosts":   len(data.Hosts),
	})
}
