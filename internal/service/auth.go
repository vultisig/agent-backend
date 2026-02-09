package service

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

// TokenTypeAccess is the expected token type for access tokens.
const TokenTypeAccess = "access"

// Claims represents the JWT claims structure used by the verifier.
type Claims struct {
	jwt.RegisteredClaims
	PublicKey string `json:"public_key"`
	TokenID   string `json:"token_id"`
	TokenType string `json:"token_type"`
}

// AuthService handles JWT token validation.
type AuthService struct {
	jwtSecret []byte
}

// NewAuthService creates a new AuthService with the given JWT secret.
func NewAuthService(secret string) *AuthService {
	return &AuthService{jwtSecret: []byte(secret)}
}

// ValidateToken validates a JWT token and returns the claims.
func (a *AuthService) ValidateToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return a.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid or expired token")
	}
	if claims.PublicKey == "" {
		return nil, errors.New("token missing public key")
	}
	if claims.TokenID == "" {
		return nil, errors.New("token missing token ID")
	}
	if claims.TokenType != TokenTypeAccess {
		return nil, errors.New("access token required")
	}
	return claims, nil
}
