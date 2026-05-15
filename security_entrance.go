package main

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pdai/pdai/internal/model"
	"gorm.io/gorm"
)

const securityEntranceCookie = "pdai_security_entrance"

func securityEntranceMiddleware(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		enabled, entrance := securityEntranceConfig(db)
		if !enabled || entrance == "" || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		path := c.Request.URL.Path
		if isSecurityEntrancePath(path, entrance) {
			http.SetCookie(c.Writer, &http.Cookie{
				Name:     securityEntranceCookie,
				Value:    entrance,
				Path:     "/",
				MaxAge:   86400 * 30,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https",
			})
			c.Next()
			return
		}

		if !requiresSecurityEntrance(path) {
			c.Next()
			return
		}

		if hasSecurityEntranceCookie(c, entrance) {
			c.Next()
			return
		}

		c.AbortWithStatus(http.StatusNotFound)
	}
}

func securityEntranceConfig(db *gorm.DB) (bool, string) {
	var enabledSetting model.Setting
	if err := db.Where("key = ?", "security_entrance_enabled").First(&enabledSetting).Error; err != nil {
		return false, ""
	}
	if enabledSetting.Value != "true" {
		return false, ""
	}

	var pathSetting model.Setting
	if err := db.Where("key = ?", "security_entrance_path").First(&pathSetting).Error; err != nil {
		return false, ""
	}
	entrance := strings.Trim(strings.TrimSpace(pathSetting.Value), "/")
	if entrance == "" {
		return false, ""
	}
	return true, entrance
}

func isSecurityEntrancePath(path, entrance string) bool {
	clean := "/" + strings.Trim(strings.TrimSpace(entrance), "/")
	return path == clean || path == clean+"/"
}

func hasSecurityEntranceCookie(c *gin.Context, entrance string) bool {
	value, err := c.Cookie(securityEntranceCookie)
	return err == nil && value == entrance
}

func requiresSecurityEntrance(path string) bool {
	if strings.HasPrefix(path, "/api/") {
		return path == "/api/auth/login" || path == "/api/auth/setup"
	}
	if path == "/favicon.ico" || strings.HasPrefix(path, "/assets/") {
		return false
	}
	if ext := filepath.Ext(path); ext != "" {
		return false
	}
	return true
}
