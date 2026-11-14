package entity

import "errors"

var (
	ErrNotFound      = errors.New("link not found")
	ErrInvalidURL    = errors.New("invalid url")
	ErrCodeCollision = errors.New("could not generate a unique code")
)
