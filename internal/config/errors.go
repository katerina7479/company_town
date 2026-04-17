// Package config provides configuration loading and validation for Company Town.
package config

import "errors"

// ErrInvalidRepo is returned when the repo config value is absent, a
// placeholder, a URL, or missing the owner/repo slash.
var ErrInvalidRepo = errors.New("invalid repo")

// ErrInvalidPlatform is returned when the platform config value is absent or
// not one of the recognized values ("github", "gitlab").
var ErrInvalidPlatform = errors.New("invalid platform")

// ErrInvalidTicketTransition is returned when a ticket_transition entry has an
// empty or duplicate from/to field.
var ErrInvalidTicketTransition = errors.New("invalid ticket_transition")
