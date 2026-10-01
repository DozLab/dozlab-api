package handlers

import "github.com/gin-gonic/gin"

// Roles a user can have (models.User.Role). AuthMiddleware puts the token's role in the
// request context as "role".
const (
	RoleAdmin      = "admin"
	RoleInstructor = "instructor"
	RoleStudent    = "student"
)

// canManageLabs reports whether the caller may create labs and set session options: only
// instructors and admins (docs/decision.md, "What phase 1 needs in the API"). A missing or
// unknown role gets a student's rights.
func canManageLabs(c *gin.Context) bool {
	role, _ := c.Get("role")
	return role == RoleAdmin || role == RoleInstructor
}
