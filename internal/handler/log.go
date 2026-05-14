package handler

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pdai/pdai/internal/config"
)

// LogHandler manages log viewing endpoints
type LogHandler struct {
	cfg *config.Config
}

// NewLogHandler creates a new LogHandler
func NewLogHandler(cfg *config.Config) *LogHandler {
	return &LogHandler{cfg: cfg}
}

// GetLogs returns the last N lines from a log file
func (h *LogHandler) GetLogs(c *gin.Context) {
	logType := c.DefaultQuery("type", "caddy")
	linesStr := c.DefaultQuery("lines", "100")
	search := c.DefaultQuery("search", "")

	lines, _ := strconv.Atoi(linesStr)
	if lines <= 0 || lines > 5000 {
		lines = 100
	}

	logFile := h.resolveLogFile(logType)
	if logFile == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid log type"})
		return
	}

	content, err := tailFile(logFile, lines, search)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"lines": []string{}, "file": logFile, "error": "Log file not found or empty"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"lines": content,
		"file":  logFile,
		"total": len(content),
	})
}

// ListLogFiles returns available log files
func (h *LogHandler) ListLogFiles(c *gin.Context) {
	entries, err := os.ReadDir(h.cfg.LogDir)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"files": []string{}})
		return
	}

	var files []map[string]interface{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, _ := e.Info()
		files = append(files, map[string]interface{}{
			"name": e.Name(),
			"size": info.Size(),
		})
	}

	c.JSON(http.StatusOK, gin.H{"files": files})
}

// Download serves a log file for download
func (h *LogHandler) Download(c *gin.Context) {
	logType := c.DefaultQuery("type", "caddy")
	logFile := h.resolveLogFile(logType)

	if logFile == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid log type"})
		return
	}

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Log file not found"})
		return
	}

	c.FileAttachment(logFile, filepath.Base(logFile))
}

func (h *LogHandler) resolveLogFile(logType string) string {
	// Sanitize: prevent path traversal
	logType = filepath.Base(logType)

	switch {
	case logType == "app" || logType == "pdai" || logType == "pdaipanel":
		return filepath.Join(h.cfg.LogDir, "Pdai.log")
	case logType == "caddy":
		return filepath.Join(h.cfg.LogDir, "caddy.log")
	case strings.HasPrefix(logType, "access-"):
		return filepath.Join(h.cfg.LogDir, logType+".log")
	case strings.HasSuffix(logType, ".log"):
		return filepath.Join(h.cfg.LogDir, logType)
	default:
		return filepath.Join(h.cfg.LogDir, logType+".log")
	}
}

// tailFile reads the last N lines from a file, optionally filtering by search term
func tailFile(filePath string, n int, search string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var allLines []string
	scanner := bufio.NewScanner(f)
	// Increase scanner buffer for long log lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if search != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(search)) {
			continue
		}
		allLines = append(allLines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	// Return last N lines
	if len(allLines) > n {
		allLines = allLines[len(allLines)-n:]
	}

	return allLines, nil
}

// GetSystemLog returns the application system log.
func (h *LogHandler) GetSystemLog(c *gin.Context) {
	linesStr := c.DefaultQuery("lines", "200")
	search := c.DefaultQuery("search", "")

	lines, _ := strconv.Atoi(linesStr)
	if lines <= 0 || lines > 5000 {
		lines = 200
	}

	filterLines := func(content []string) []string {
		if search == "" {
			return content
		}
		filtered := make([]string, 0, len(content))
		needle := strings.ToLower(search)
		for _, line := range content {
			if strings.Contains(strings.ToLower(line), needle) {
				filtered = append(filtered, line)
			}
		}
		return filtered
	}

	readJournal := func(args ...string) ([]string, error) {
		cmd := exec.Command("journalctl", args...)
		output, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		text := strings.TrimSpace(string(output))
		if text == "" {
			return []string{}, nil
		}
		return strings.Split(text, "\n"), nil
	}

	journalTargets := [][]string{
		{"-u", "pdai", "--no-pager", "-n", strconv.Itoa(lines), "--output", "short-iso"},
		{"-u", "haispalen", "--no-pager", "-n", strconv.Itoa(lines), "--output", "short-iso"},
		{"-u", "Pdai", "--no-pager", "-n", strconv.Itoa(lines), "--output", "short-iso"},
		{"-t", "haispalen", "--no-pager", "-n", strconv.Itoa(lines), "--output", "short-iso"},
	}
	for _, target := range journalTargets {
		if content, err := readJournal(target...); err == nil {
			content = filterLines(content)
			joined := strings.Join(content, "\n")
			c.JSON(http.StatusOK, gin.H{"lines": content, "content": joined, "source": "journalctl", "total": len(content)})
			return
		}
	}

	logFiles := []string{
		filepath.Join(h.cfg.LogDir, "pdai.log"),
		filepath.Join(h.cfg.LogDir, "Pdai.log"),
		filepath.Join(h.cfg.LogDir, "pdai.err"),
		filepath.Join(h.cfg.LogDir, "Pdai.err"),
	}
	for _, logFile := range logFiles {
		content, err := tailFile(logFile, lines, search)
		if err == nil {
			joined := strings.Join(content, "\n")
			c.JSON(http.StatusOK, gin.H{"lines": content, "content": joined, "source": "file", "file": logFile, "total": len(content)})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"lines": []string{}, "content": "", "source": "file", "error": "System log not found"})
}
