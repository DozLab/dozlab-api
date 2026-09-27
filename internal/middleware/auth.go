package middleware

import (
	"net/http"
	"strings"

	"dozlab-backend/pkg/auth"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func AuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header is required",
			})
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Bearer token is required",
			})
			c.Abort()
			return
		}

		authenticate(c, tokenString, jwtSecret)
	})
}

// WebSocketSubprotocol is the Sec-WebSocket-Protocol value that precedes the JWT
// when a browser connects: new WebSocket(url, ["dozlab.bearer", token]).
// The server echoes only this value, never the token (see docs/decision.md).
const WebSocketSubprotocol = "dozlab.bearer"

// WebSocketAuthMiddleware authenticates a WebSocket upgrade request with
// "Authorization: Bearer <JWT>" or, for browsers, which can't set that header,
// "Sec-WebSocket-Protocol: dozlab.bearer, <JWT>".
func WebSocketAuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		tokenString := ""
		if authHeader := c.GetHeader("Authorization"); authHeader != "" {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader {
				tokenString = ""
			}
		} else {
			tokenString = subprotocolToken(websocket.Subprotocols(c.Request))
		}
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Bearer token is required (Authorization header or " + WebSocketSubprotocol + " subprotocol)",
			})
			c.Abort()
			return
		}

		authenticate(c, tokenString, jwtSecret)
	})
}

// subprotocolToken returns the value that follows WebSocketSubprotocol, or "".
func subprotocolToken(protocols []string) string {
	for i, p := range protocols {
		if p == WebSocketSubprotocol && i+1 < len(protocols) {
			return protocols[i+1]
		}
	}
	return ""
}

// authenticate validates the JWT and sets the user in the context, or aborts with 401.
func authenticate(c *gin.Context, tokenString, jwtSecret string) {
	claims, err := auth.ValidateToken(tokenString, jwtSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid or expired token",
		})
		c.Abort()
		return
	}

	// Set user information in context
	c.Set("user_id", claims.UserID)
	c.Set("username", claims.Username)
	c.Set("email", claims.Email)
	c.Set("role", claims.Role)

	c.Next()
}

func RoleMiddleware(requiredRole string) gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		userRole, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "User role not found in context",
			})
			c.Abort()
			return
		}

		if userRole != requiredRole {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Insufficient privileges",
			})
			c.Abort()
			return
		}

		c.Next()
	})
}