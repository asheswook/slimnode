package state

import "errors"

var (
	ErrInvalidTransition = errors.New("invalid state transition")
)
