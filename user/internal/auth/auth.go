package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/nikivavlt/url-shortener/user/internal/service"
	"golang.org/x/crypto/bcrypt"
)

const issuer = "url-shortener-user"

var ErrInvalidToken = errors.New("invalid token")

type tokenClaims struct {
	SID       string `json:"sid"`
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

type Auth struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func New(secret string, accessTTL, refreshTTL time.Duration) *Auth {
	return &Auth{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (a *Auth) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func (a *Auth) ComparePassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func (a *Auth) Sign(userID, sid string, tokenType service.TokenType) (string, error) {
	var ttl time.Duration
	switch tokenType {
	case service.TokenTypeAccess:
		ttl = a.accessTTL
	case service.TokenTypeRefresh:
		ttl = a.refreshTTL
	default:
		return "", fmt.Errorf("unsupported token type %q", tokenType)
	}

	now := time.Now()
	claims := tokenClaims{
		SID:       sid,
		TokenType: string(tokenType),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(a.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

func (a *Auth) Parse(token string, tokenType service.TokenType) (userID string, sid string, err error) {
	var claims tokenClaims

	_, err = jwt.ParseWithClaims(
		token,
		&claims,
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return a.secret, nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(issuer),
	)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if claims.TokenType != string(tokenType) {
		return "", "", fmt.Errorf("%w: unexpected token type", ErrInvalidToken)
	}

	return claims.Subject, claims.SID, nil
}
