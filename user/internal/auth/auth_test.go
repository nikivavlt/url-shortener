package auth

import (
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	auth := New("secret", time.Minute, time.Hour)

	hash, err := auth.HashPassword("password")
	if err != nil {
		t.Fatal(err)
	}

	if err := auth.ComparePassword(hash, "password"); err != nil {
		t.Fatal("password should match")
	}

	if err := auth.ComparePassword(hash, "wrong"); err == nil {
		t.Fatal("wrong password should not match")
	}
}

func TestAccessToken(t *testing.T) {
	auth := New("secret", time.Minute, time.Hour)

	token, err := auth.SignAccess("user-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}

	userID, sid, err := auth.Parse(token)
	if err != nil {
		t.Fatal(err)
	}

	if userID != "user-1" {
		t.Errorf("userID = %q, want %q", userID, "user-1")
	}

	if sid != "session-1" {
		t.Errorf("sid = %q, want %q", sid, "session-1")
	}
}

func TestRefreshToken(t *testing.T) {
	auth := New("secret", time.Minute, time.Hour)

	token, err := auth.SignRefresh("user-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}

	userID, sid, err := auth.Parse(token)
	if err != nil {
		t.Fatal(err)
	}

	if userID != "user-1" || sid != "session-1" {
		t.Errorf("got userID=%q sid=%q", userID, sid)
	}
}

func TestParseInvalidToken(t *testing.T) {
	auth := New("secret", time.Minute, time.Hour)

	_, _, err := auth.Parse("invalid-token")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseWrongSecret(t *testing.T) {
	auth := New("secret", time.Minute, time.Hour)
	other := New("other-secret", time.Minute, time.Hour)

	token, err := auth.SignAccess("user-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = other.Parse(token)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseExpiredToken(t *testing.T) {
	auth := New("secret", -time.Minute, time.Hour)

	token, err := auth.SignAccess("user-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = auth.Parse(token)
	if err == nil {
		t.Fatal("expected expired token error")
	}
}
