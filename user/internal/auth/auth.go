package auth

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Auth struct {
	secret     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func (a Auth) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {

	}

	return string(hash), nil

}

func (a Auth) ComparePassword(hash, password string) error {
	err := bcrypt.CompareHashAndPassword(
		[]byte(hash),
		[]byte(password),
	)

	if err != nil {
		// password is incorrect
		// return ErrInvalidCredentials
	}

	return nil
}

// func (a Auth) SignAccess(userID, sid string) (string, error){}

// func (a Auth) SignRefresh(userID, sid string) (string, error){}

// func (a Auth) Parse(token string) (string, error){}
