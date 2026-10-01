package middleware

import (
	"errors"
	"net/http"

	"dozlab-backend/internal/audit"
	"dozlab-backend/internal/authz"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CurrentUser replaces the role from the token with the user's role in the database, and
// refuses users who were deactivated or deleted after the token was issued. Without it, a
// change to a user's role or status only takes effect when their token expires. Use it after
// AuthMiddleware or WebSocketAuthMiddleware.
func CurrentUser(db *database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
			c.Abort()
			return
		}

		var user models.User
		err := db.DB.Select("id", "role", "is_active").First(&user, "id = ?", userID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User no longer exists"})
			c.Abort()
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			c.Abort()
			return
		}
		if !user.IsActive {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User account is inactive"})
			c.Abort()
			return
		}

		c.Set("role", user.Role)
		c.Next()
	}
}

// RequirePermission lets the request through only if the caller's role has the permission
// (authz), and names the permission as the request's action in the audit log.
func RequirePermission(p authz.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		audit.Annotate(c).Action = string(p)

		role, _ := c.Get("role")
		roleName, _ := role.(string)
		if !authz.Can(authz.Role(roleName), p) {
			c.JSON(http.StatusForbidden, gin.H{
				"error":      "Insufficient privileges",
				"permission": string(p),
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
