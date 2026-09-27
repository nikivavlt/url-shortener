package domain

import "errors"

var (
	ErrInvalidArgument        = errors.New("invalid argument")
	ErrLinkNotFound           = errors.New("link not found")
	ErrPermissionDenied       = errors.New("permission denied")
	ErrLimitExceeded          = errors.New("links limit exceeded")
	ErrUserNotFound           = errors.New("user not found")
	ErrUserServiceUnavailable = errors.New("user service unavailable")
	ErrShortCodeTaken         = errors.New("short code already taken")
	ErrCacheMiss              = errors.New("cache miss")
)
