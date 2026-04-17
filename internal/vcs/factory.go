package vcs

import (
	"fmt"

	"github.com/katerina7479/company_town/internal/vcs/gitlab"
)

// ProviderFromConfig returns a Provider for the given platform and repository
// reference. An empty platform defaults to "github" for backward compatibility.
// For GitHub, repoRef is unused (gh reads the remote from the local git config).
// For GitLab, repoRef must be the project path (e.g. "kate/myproj").
func ProviderFromConfig(platform, repoRef string) (Provider, error) {
	switch platform {
	case "github", "":
		return NewGitHub(), nil
	case "gitlab":
		return gitlab.New(repoRef), nil
	default:
		return nil, fmt.Errorf("vcs: unknown platform %q — valid values are \"github\" and \"gitlab\"", platform)
	}
}
