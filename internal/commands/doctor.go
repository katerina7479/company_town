package commands

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/session"
)

var (
	doctorOkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // bright green
	doctorWarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // bright yellow
	doctorFailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // bright red
)

// checkResult holds the outcome of a single doctor check.
type checkResult struct {
	Name   string
	Status string // "ok", "fail", "warn"
	Detail string
	Fix    string // copy-pasteable fix command, if any
}

// doctorDeps groups injected functions for testability.
type doctorDeps struct {
	runCmd        func(name string, args ...string) (string, error)
	loadConfig    func(root string) (*config.Config, error)
	findRoot      func() (string, error)
	sessionExists func(name string) bool
}

func defaultDoctorDeps() doctorDeps {
	return doctorDeps{
		runCmd: func(name string, args ...string) (string, error) {
			out, err := exec.Command(name, args...).CombinedOutput()
			return strings.TrimSpace(string(out)), err
		},
		loadConfig: func(root string) (*config.Config, error) {
			return config.Load(root)
		},
		findRoot:      db.FindProjectRoot,
		sessionExists: session.Exists,
	}
}

// parseVersion extracts the first "X.Y.Z" or "X.Y" version string from s.
func parseVersion(s string) (major, minor, patch int, ok bool) {
	words := strings.Fields(s)
	for _, w := range words {
		w = strings.TrimPrefix(w, "v")
		parts := strings.SplitN(w, ".", 3)
		if len(parts) < 2 {
			continue
		}
		maj, err1 := strconv.Atoi(parts[0])
		min, err2 := strconv.Atoi(strings.TrimRight(parts[1], ",;:"))
		if err1 != nil || err2 != nil {
			continue
		}
		pat := 0
		if len(parts) == 3 {
			pat, _ = strconv.Atoi(strings.TrimRight(parts[2], ",;:"))
		}
		return maj, min, pat, true
	}
	return 0, 0, 0, false
}

// versionAtLeast returns true if (maj, min, pat) >= (reqMaj, reqMin, reqPat).
func versionAtLeast(maj, min, pat, reqMaj, reqMin, reqPat int) bool {
	if maj != reqMaj {
		return maj > reqMaj
	}
	if min != reqMin {
		return min > reqMin
	}
	return pat >= reqPat
}

func checkDolt(deps doctorDeps) checkResult {
	out, err := deps.runCmd("dolt", "version")
	if err != nil {
		return checkResult{
			Name:   "dolt",
			Status: "fail",
			Detail: "not found",
			Fix:    "curl -L https://github.com/dolthub/dolt/releases/latest/download/install.sh | sudo bash",
		}
	}
	maj, min, pat, ok := parseVersion(out)
	if !ok {
		return checkResult{Name: "dolt", Status: "warn", Detail: "version unknown: " + out}
	}
	ver := fmt.Sprintf("%d.%d.%d", maj, min, pat)
	if !versionAtLeast(maj, min, pat, 1, 0, 0) {
		return checkResult{
			Name:   "dolt",
			Status: "fail",
			Detail: ver + " (need >= 1.0.0)",
			Fix:    "curl -L https://github.com/dolthub/dolt/releases/latest/download/install.sh | sudo bash",
		}
	}
	return checkResult{Name: "dolt", Status: "ok", Detail: ver}
}

func checkTmux(deps doctorDeps) checkResult {
	out, err := deps.runCmd("tmux", "-V")
	if err != nil {
		return checkResult{
			Name:   "tmux",
			Status: "fail",
			Detail: "not found",
			Fix:    "brew install tmux",
		}
	}
	maj, min, _, ok := parseVersion(out)
	if !ok {
		return checkResult{Name: "tmux", Status: "warn", Detail: "version unknown: " + out}
	}
	ver := fmt.Sprintf("%d.%d", maj, min)
	if !versionAtLeast(maj, min, 0, 3, 0, 0) {
		return checkResult{
			Name:   "tmux",
			Status: "fail",
			Detail: ver + " (need >= 3.0)",
			Fix:    "brew upgrade tmux",
		}
	}
	return checkResult{Name: "tmux", Status: "ok", Detail: ver}
}

// checkVCSCLI checks that the platform's CLI tool is installed and authenticated.
// platform must be config.PlatformGitHub or config.PlatformGitLab.
func checkVCSCLI(deps doctorDeps, platform string) checkResult {
	if platform == config.PlatformGitLab {
		_, err := deps.runCmd("glab", "--version")
		if err != nil {
			return checkResult{
				Name:   "glab",
				Status: "fail",
				Detail: "not found",
				Fix:    "brew install glab",
			}
		}
		_, authErr := deps.runCmd("glab", "auth", "status")
		if authErr != nil {
			return checkResult{
				Name:   "glab",
				Status: "fail",
				Detail: "not authenticated",
				Fix:    "glab auth login",
			}
		}
		return checkResult{Name: "glab", Status: "ok", Detail: "authenticated"}
	}

	_, err := deps.runCmd("gh", "--version")
	if err != nil {
		return checkResult{
			Name:   "gh",
			Status: "fail",
			Detail: "not found",
			Fix:    "brew install gh",
		}
	}
	_, authErr := deps.runCmd("gh", "auth", "status")
	if authErr != nil {
		return checkResult{
			Name:   "gh",
			Status: "fail",
			Detail: "not authenticated",
			Fix:    "gh auth login",
		}
	}
	return checkResult{Name: "gh", Status: "ok", Detail: "authenticated"}
}

func checkGit(deps doctorDeps) checkResult {
	out, err := deps.runCmd("git", "--version")
	if err != nil {
		return checkResult{
			Name:   "git",
			Status: "fail",
			Detail: "not found",
			Fix:    "brew install git",
		}
	}
	maj, min, pat, ok := parseVersion(out)
	if !ok {
		return checkResult{Name: "git", Status: "warn", Detail: "version unknown: " + out}
	}
	ver := fmt.Sprintf("%d.%d.%d", maj, min, pat)
	if !versionAtLeast(maj, min, pat, 2, 30, 0) {
		return checkResult{
			Name:   "git",
			Status: "fail",
			Detail: ver + " (need >= 2.30)",
			Fix:    "brew upgrade git",
		}
	}
	return checkResult{Name: "git", Status: "ok", Detail: ver}
}

func checkConfig(deps doctorDeps) (checkResult, *config.Config) {
	root, err := deps.findRoot()
	if err != nil {
		return checkResult{
			Name:   "config",
			Status: "warn",
			Detail: "not inside a Company Town project",
		}, nil
	}

	cfg, err := deps.loadConfig(root)
	if err != nil {
		return checkResult{
			Name:   "config",
			Status: "fail",
			Detail: err.Error(),
			Fix:    "ct init",
		}, nil
	}

	var missing []string
	if cfg.TicketPrefix == "" {
		missing = append(missing, "ticket_prefix")
	}
	if cfg.ProjectRoot == "" {
		missing = append(missing, "project_root")
	}
	switch cfg.EffectivePlatform() {
	case config.PlatformGitLab:
		if cfg.GitlabProject == "" {
			missing = append(missing, "gitlab_project")
		}
	default:
		if cfg.GithubRepo == "" {
			missing = append(missing, "github_repo")
		}
	}
	if cfg.Agents.Mayor.Model == "" {
		missing = append(missing, "agents.mayor.model")
	}
	if len(missing) > 0 {
		return checkResult{
			Name:   "config",
			Status: "fail",
			Detail: "missing required fields: " + strings.Join(missing, ", "),
			Fix:    "edit .company_town/config.json",
		}, cfg
	}

	return checkResult{Name: "config", Status: "ok", Detail: ".company_town/config.json ok"}, cfg
}

func checkDaemon(deps doctorDeps) checkResult {
	daemonSession := session.SessionName("daemon")
	if !deps.sessionExists(daemonSession) {
		return checkResult{
			Name:   "daemon",
			Status: "warn",
			Detail: "not running",
			Fix:    "ct start",
		}
	}
	return checkResult{Name: "daemon", Status: "ok", Detail: "running"}
}

func runDoctor(deps doctorDeps) ([]checkResult, bool) {
	results := []checkResult{
		checkDolt(deps),
		checkTmux(deps),
		checkGit(deps),
	}

	cfgResult, cfg := checkConfig(deps)

	platform := config.PlatformGitHub
	if cfg != nil {
		platform = cfg.EffectivePlatform()
	}
	results = append(results, checkVCSCLI(deps, platform))
	results = append(results, cfgResult)

	// Only check daemon if we're inside a project.
	if cfg != nil {
		if cfg.SessionPrefix != "" {
			session.SessionPrefix = cfg.SessionPrefix
		}
		results = append(results, checkDaemon(deps))
	}

	anyFail := false
	for _, r := range results {
		if r.Status == "fail" {
			anyFail = true
			break
		}
	}
	return results, anyFail
}

// Doctor implements `ct doctor` -- checks system dependencies and project setup.
func Doctor() error {
	deps := defaultDoctorDeps()
	results, anyFail := runDoctor(deps)
	printDoctorResults(results)
	if anyFail {
		return fmt.Errorf("one or more checks failed")
	}
	return nil
}

func printDoctorResults(results []checkResult) {
	for _, r := range results {
		var icon string
		switch r.Status {
		case "ok":
			icon = doctorOkStyle.Render("\u2713")
		case "warn":
			icon = doctorWarnStyle.Render("!")
		default:
			icon = doctorFailStyle.Render("\u2717")
		}
		line := fmt.Sprintf("%s %-12s %s", icon, r.Name, r.Detail)
		if r.Fix != "" {
			line += fmt.Sprintf(" -- run '%s'", r.Fix)
		}
		fmt.Println(line)
	}
}
