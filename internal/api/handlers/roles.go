package handlers

import (
	"dozlab-backend/internal/authz"

	"github.com/gin-gonic/gin"
)

// can reports whether the caller's role has the permission (internal/authz). The role is put
// in the request context by AuthMiddleware and refreshed from the database by CurrentUser; a
// missing or unknown role has no permissions.
func can(c *gin.Context, p authz.Permission) bool {
	role, _ := c.Get("role")
	roleName, _ := role.(string)
	return authz.Can(authz.Role(roleName), p)
}
