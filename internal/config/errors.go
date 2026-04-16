// Package config provides configuration loading and validation for Company Town.
package config

import "errors"

// ErrInvalidGithubRepo is returned when the github_repo config value is absent,
// a placeholder, a URL, or missing the owner/repo slash.
var ErrInvalidGithubRepo = errors.New("invalid github_repo")

// ErrInvalidGitlabProject is returned when the gitlab_project config value is
// absent, a placeholder, a URL, or missing the namespace/project slash.
var ErrInvalidGitlabProject = errors.New("invalid gitlab_project")

// ErrInvalidTicketTransition is returned when a ticket_transition entry has an
// empty or duplicate from/to field.
var ErrInvalidTicketTransition = errors.New("invalid ticket_transition")
