package vcs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GitHub implements Provider using the gh CLI.
type GitHub struct{}

// NewGitHub returns a GitHub VCS provider backed by the gh CLI.
func NewGitHub() Provider {
	return &GitHub{}
}

func (g *GitHub) CreatePR(title, body string, draft bool, repoDir string) (string, error) {
	ghArgs := []string{"pr", "create", "--title", title, "--body", body}
	if draft {
		ghArgs = append(ghArgs, "--draft")
	}
	cmd := exec.Command("gh", ghArgs...)
	cmd.Dir = repoDir
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *GitHub) GetPRMetadata(prNum int, repoDir string) ([]byte, error) {
	fields := "number,title,state,headRefName,mergeable,reviewDecision,statusCheckRollup"
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--json", fields)
	cmd.Dir = repoDir
	return cmd.Output()
}

func (g *GitHub) GetPRReviews(prNum int, repoDir string) ([]byte, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--json", "reviews")
	cmd.Dir = repoDir
	return cmd.Output()
}

func (g *GitHub) GetPRComments(prNum int, repoDir string) ([]byte, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--json", "comments")
	cmd.Dir = repoDir
	return cmd.Output()
}

func (g *GitHub) MarkPRReady(prNum int, repoDir string) error {
	cmd := exec.Command("gh", "pr", "ready", strconv.Itoa(prNum))
	cmd.Dir = repoDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (g *GitHub) ClosePR(prNum int, repoDir string) error {
	cmd := exec.Command("gh", "pr", "close", strconv.Itoa(prNum),
		"--comment", "Ticket cancelled.")
	cmd.Dir = repoDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ansiRe matches ANSI CSI escape sequences (e.g. color codes from gh output).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func cleanStderr(s string) string {
	s = ansiRe.ReplaceAllString(strings.TrimSpace(s), "")
	const limit = 200
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}

func (g *GitHub) OpenPRInBrowser(prNum int, repoDir string) error {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--web")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) > 0 {
		return fmt.Errorf("%w: %s", err, cleanStderr(string(out)))
	}
	return err
}

func (g *GitHub) GetPRHeadBranch(prNum int, repoDir string) (string, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum),
		"--json", "headRefName", "--jq", ".headRefName")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh pr view %d: %w", prNum, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *GitHub) GetPRStateJSON(prNum int, repoDir string) ([]byte, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum),
		"--json", "state,mergedAt,mergeable,statusCheckRollup")
	cmd.Dir = repoDir
	return runCmd(cmd)
}

func (g *GitHub) GetReviewCommentsRaw(prNum int, repoDir string) ([]byte, error) {
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/reviews", prNum),
		"--jq", ".[] | {author: .user.login, authorType: .user.type, state: .state, body: .body}")
	cmd.Dir = repoDir
	return runCmd(cmd)
}

// ghPRListEntry is one element from `gh pr list --json number,state,updatedAt`.
type ghPRListEntry struct {
	Number    int       `json:"number"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (g *GitHub) FindPRByBranch(branch, repoDir string) (int, bool, error) {
	cmd := exec.Command("gh", "pr", "list",
		"--head", branch,
		"--state", "all",
		"--json", "number,state,updatedAt",
		"--limit", "5",
	)
	cmd.Dir = repoDir
	out, err := runCmd(cmd)
	if err != nil {
		return 0, false, fmt.Errorf("gh pr list: %w", err)
	}

	var results []ghPRListEntry
	if err := json.Unmarshal(out, &results); err != nil {
		return 0, false, fmt.Errorf("parsing PR list: %w", err)
	}
	if len(results) == 0 {
		return 0, false, nil
	}
	return pickMostRecentPR(results), true, nil
}

// pickMostRecentPR selects the most authoritative PR from a list. It sorts by
// UpdatedAt descending; ties are broken by state precedence: MERGED > OPEN > CLOSED.
func pickMostRecentPR(entries []ghPRListEntry) int {
	if len(entries) == 0 {
		return 0
	}
	statePrecedence := func(s string) int {
		switch s {
		case "MERGED":
			return 0
		case "OPEN":
			return 1
		default:
			return 2
		}
	}
	best := entries[0]
	for _, e := range entries[1:] {
		if e.UpdatedAt.After(best.UpdatedAt) {
			best = e
		} else if e.UpdatedAt.Equal(best.UpdatedAt) && statePrecedence(e.State) < statePrecedence(best.State) {
			best = e
		}
	}
	return best.Number
}

// runCmd runs cmd, captures stderr for error messages, and returns stdout.
func runCmd(cmd *exec.Cmd) ([]byte, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		snippet := strings.TrimSpace(stderr.String())
		const stderrSnippetLen = 200
		if len(snippet) > stderrSnippetLen {
			snippet = snippet[:stderrSnippetLen] + "..."
		}
		if snippet != "" {
			return nil, fmt.Errorf("%w: %s", err, snippet)
		}
		return nil, err
	}
	return out, nil
}
