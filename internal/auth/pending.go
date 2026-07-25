package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const Pending2FAPurpose = "admin_2fa"

type Pending2FAClaims struct {
	UserID  uint   `json:"uid"`
	Purpose string `json:"purpose"`
	jwt.RegisteredClaims
}

func GeneratePending2FAToken(secret string, userID uint, now time.Time) (string, error) {
	claims := Pending2FAClaims{
		UserID:  userID,
		Purpose: Pending2FAPurpose,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ParsePending2FAToken(secret, raw string) (*Pending2FAClaims, error) {
	token, err := jwt.ParseWithClaims(raw, &Pending2FAClaims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Pending2FAClaims)
	if !ok || !token.Valid || claims.Purpose != Pending2FAPurpose || claims.UserID == 0 {
		return nil, fmt.Errorf("invalid pending token")
	}
	return claims, nil
}
