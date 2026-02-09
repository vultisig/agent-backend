package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// AuthMiddleware validates JWT tokens and extracts the public key.
func (s *Server) AuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		authHeader := c.Request().Header.Get(echo.HeaderAuthorization)
		if authHeader == "" {
			return c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "missing authorization header"})
		}

		parts := strings.Fields(authHeader)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "invalid authorization header"})
		}

		claims, err := s.authService.ValidateToken(parts[1])
		if err != nil {
			return c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "invalid token"})
		}

		c.Set("public_key", claims.PublicKey)
		return next(c)
	}
}

// GetPublicKey extracts the public key from the echo context.
func GetPublicKey(c echo.Context) string {
	pk, _ := c.Get("public_key").(string)
	return pk
}

// GetAccessToken extracts the raw JWT from Authorization header.
func GetAccessToken(c echo.Context) string {
	auth := c.Request().Header.Get("Authorization")
	if token, found := strings.CutPrefix(auth, "Bearer "); found {
		return token
	}
	return ""
}
