package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pdai/pdai/internal/model"
	"gorm.io/gorm"
)

const (
	RoleAdmin = "admin"
)

// ValidRoles is the set of valid role values.
var ValidRoles = map[string]bool{
	RoleAdmin: true,
}

// roleLevel returns the privilege level for a role (higher = more access).
func roleLevel(role string) int {
	switch role {
	case RoleAdmin:
		return 1
	default:
		return 0
	}
}

// requireRole creates a middleware that requires at least the given role level.
func requireRole(db *gorm.DB, minRole string, errorMsg string) gin.HandlerFunc {
	minLevel := roleLevel(minRole)
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
			c.Abort()
			return
		}

		var user model.User
		if err := db.Select("id, role").First(&user, userID).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			c.Abort()
			return
		}

		userLevel := roleLevel(user.Role)
		if userLevel < minLevel {
			c.JSON(http.StatusForbidden, gin.H{"error": errorMsg})
			c.Abort()
			return
		}

		c.Set("user_role", user.Role)
		c.Next()
	}
}

// RequireAdmin restricts access to admin-role users.
// Use for: configuration changes, user management, certificate management.
func RequireAdmin(db *gorm.DB) gin.HandlerFunc {
	return requireRole(db, RoleAdmin, "Admin access required")
}

// RequireOperator is kept for plugin route compatibility; admin is the only
// privileged role.
func RequireOperator(db *gorm.DB) gin.HandlerFunc {
	return requireRole(db, RoleAdmin, "Admin access required")
}
