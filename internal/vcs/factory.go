package vcs

import "fmt"

// ProviderFromConfig returns a Provider for the given platform string.
// An empty platform defaults to "github" for backward compatibility.
// Returns an error for unsupported platforms.
func ProviderFromConfig(platform string) (Provider, error) {
	switch platform {
	case "github", "":
		return NewGitHub(), nil
	case "gitlab":
		return nil, fmt.Errorf("vcs: GitLab provider not yet implemented (see nc-233)")
	default:
		return nil, fmt.Errorf("vcs: unknown platform %q — valid values are \"github\" and \"gitlab\"", platform)
	}
}
