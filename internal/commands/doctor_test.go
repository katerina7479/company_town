package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
)

// --- parseVersion ---

func TestParseVersion(t *testing.T) {
	cases := []struct {
		input string
		maj   int
		min   int
		pat   int
		ok    bool
	}{
		{"dolt version 1.50.1", 1, 50, 1, true},
		{"tmux 3.4", 3, 4, 0, true},
		{"git version 2.47.0", 2, 47, 0, true},
		{"gh version 2.62.0 (2024-12-05)", 2, 62, 0, true},
		{"no version here", 0, 0, 0, false},
	}
	for _, tc := range cases {
		maj, min, pat, ok := parseVersion(tc.input)
		if ok != tc.ok || maj != tc.maj || min != tc.min || pat != tc.pat {
			t.Errorf("parseVersion(%q) = %d.%d.%d ok=%v; want %d.%d.%d ok=%v",
				tc.input, maj, min, pat, ok, tc.maj, tc.min, tc.pat, tc.ok)
		}
	}
}

// --- versionAtLeast ---

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		maj, min, pat          int
		reqMaj, reqMin, reqPat int
		want                   bool
	}{
		{1, 50, 1, 1, 0, 0, true},
		{0, 9, 0, 1, 0, 0, false},
		{3, 4, 0, 3, 0, 0, true},
		{2, 29, 9, 2, 30, 0, false},
		{2, 30, 0, 2, 30, 0, true},
	}
	for _, tc := range cases {
		got := versionAtLeast(tc.maj, tc.min, tc.pat, tc.reqMaj, tc.reqMin, tc.reqPat)
		if got != tc.want {
			t.Errorf("versionAtLeast(%d,%d,%d, %d,%d,%d) = %v; want %v",
				tc.maj, tc.min, tc.pat, tc.reqMaj, tc.reqMin, tc.reqPat, got, tc.want)
		}
	}
}

// --- checkDolt ---

func TestCheckDolt(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		err      error
		wantStat string
	}{
		{"ok", "dolt version 1.50.1", nil, "ok"},
		{"not found", "", errors.New("not found"), "fail"},
		{"old version", "dolt version 0.9.0", nil, "fail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := doctorDeps{
				runCmd: func(name string, args ...string) (string, error) {
					return tc.out, tc.err
				},
			}
			r := checkDolt(deps)
			if r.Status != tc.wantStat {
				t.Errorf("status=%q want=%q detail=%q", r.Status, tc.wantStat, r.Detail)
			}
			if tc.wantStat == "fail" && r.Fix == "" {
				t.Error("expected Fix to be set on fail")
			}
		})
	}
}

// --- checkTmux ---

func TestCheckTmux(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		err      error
		wantStat string
	}{
		{"ok", "tmux 3.4", nil, "ok"},
		{"not found", "", errors.New("not found"), "fail"},
		{"too old", "tmux 2.9", nil, "fail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := doctorDeps{
				runCmd: func(name string, args ...string) (string, error) {
					return tc.out, tc.err
				},
			}
			r := checkTmux(deps)
			if r.Status != tc.wantStat {
				t.Errorf("status=%q want=%q detail=%q", r.Status, tc.wantStat, r.Detail)
			}
		})
	}
}

// --- checkGH ---

func TestCheckGH(t *testing.T) {
	cases := []struct {
		name     string
		ghErr    error
		authErr  error
		wantStat string
	}{
		{"ok", nil, nil, "ok"},
		{"not found", errors.New("not found"), nil, "fail"},
		{"not authenticated", nil, errors.New("not logged in"), "fail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := doctorDeps{
				runCmd: func(name string, args ...string) (string, error) {
					if len(args) > 0 && args[0] == "auth" {
						return "", tc.authErr
					}
					return "gh version 2.0.0", tc.ghErr
				},
			}
			r := checkGH(deps)
			if r.Status != tc.wantStat {
				t.Errorf("status=%q want=%q detail=%q", r.Status, tc.wantStat, r.Detail)
			}
		})
	}
}

// --- checkGit ---

func TestCheckGit(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		err      error
		wantStat string
	}{
		{"ok", "git version 2.47.0", nil, "ok"},
		{"not found", "", errors.New("not found"), "fail"},
		{"too old", "git version 2.29.0", nil, "fail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := doctorDeps{
				runCmd: func(name string, args ...string) (string, error) {
					return tc.out, tc.err
				},
			}
			r := checkGit(deps)
			if r.Status != tc.wantStat {
				t.Errorf("status=%q want=%q detail=%q", r.Status, tc.wantStat, r.Detail)
			}
		})
	}
}

// --- checkConfig ---

func TestCheckConfig(t *testing.T) {
	goodCfg := &config.Config{
		TicketPrefix: "nc",
		ProjectRoot:  "/tmp/proj",
		GithubRepo:   "owner/repo",
		Agents:       config.AgentsConfig{Mayor: config.AgentConfig{Model: "claude-opus-4-6"}},
	}

	cases := []struct {
		name     string
		findErr  error
		loadCfg  *config.Config
		loadErr  error
		wantStat string
		wantCfg  bool
	}{
		{"not in project", errors.New("no project root"), nil, nil, "warn", false},
		{"config parse error", nil, nil, errors.New("bad json"), "fail", false},
		{"ok", nil, goodCfg, nil, "ok", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := doctorDeps{
				findRoot: func() (string, error) { return "/tmp/proj", tc.findErr },
				loadConfig: func(root string) (*config.Config, error) {
					return tc.loadCfg, tc.loadErr
				},
			}
			r, cfg := checkConfig(deps)
			if r.Status != tc.wantStat {
				t.Errorf("status=%q want=%q detail=%q", r.Status, tc.wantStat, r.Detail)
			}
			if tc.wantCfg && cfg == nil {
				t.Error("expected cfg to be non-nil")
			}
			if !tc.wantCfg && cfg != nil {
				t.Error("expected cfg to be nil")
			}
		})
	}
}

func TestCheckConfig_missingTicketPrefix(t *testing.T) {
	cfg := &config.Config{
		ProjectRoot: "/tmp/proj",
		GithubRepo:  "owner/repo",
		Agents:      config.AgentsConfig{Mayor: config.AgentConfig{Model: "claude-opus-4-6"}},
	}
	deps := doctorDeps{
		findRoot:   func() (string, error) { return "/tmp/proj", nil },
		loadConfig: func(root string) (*config.Config, error) { return cfg, nil },
	}
	r, _ := checkConfig(deps)
	if r.Status != "fail" {
		t.Errorf("status=%q want=fail", r.Status)
	}
	if !strings.Contains(r.Detail, "ticket_prefix") {
		t.Errorf("detail %q should mention ticket_prefix", r.Detail)
	}
}

func TestCheckConfig_missingGithubRepo(t *testing.T) {
	cfg := &config.Config{
		TicketPrefix: "nc",
		ProjectRoot:  "/tmp/proj",
		Agents:       config.AgentsConfig{Mayor: config.AgentConfig{Model: "claude-opus-4-6"}},
	}
	deps := doctorDeps{
		findRoot:   func() (string, error) { return "/tmp/proj", nil },
		loadConfig: func(root string) (*config.Config, error) { return cfg, nil },
	}
	r, _ := checkConfig(deps)
	if r.Status != "fail" {
		t.Errorf("status=%q want=fail", r.Status)
	}
	if !strings.Contains(r.Detail, "github_repo") {
		t.Errorf("detail %q should mention github_repo", r.Detail)
	}
}

func TestCheckConfig_missingMultipleFields(t *testing.T) {
	cfg := &config.Config{} // all empty
	deps := doctorDeps{
		findRoot:   func() (string, error) { return "/tmp/proj", nil },
		loadConfig: func(root string) (*config.Config, error) { return cfg, nil },
	}
	r, _ := checkConfig(deps)
	if r.Status != "fail" {
		t.Errorf("status=%q want=fail", r.Status)
	}
	for _, field := range []string{"ticket_prefix", "project_root", "github_repo", "agents.mayor.model"} {
		if !strings.Contains(r.Detail, field) {
			t.Errorf("detail %q should mention %q", r.Detail, field)
		}
	}
	if r.Fix == "" {
		t.Error("expected Fix to be set")
	}
}

// --- checkDaemon ---

func TestCheckDaemon(t *testing.T) {
	cases := []struct {
		name     string
		exists   bool
		wantStat string
	}{
		{"running", true, "ok"},
		{"not running", false, "warn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := doctorDeps{
				sessionExists: func(name string) bool { return tc.exists },
			}
			r := checkDaemon(deps)
			if r.Status != tc.wantStat {
				t.Errorf("status=%q want=%q", r.Status, tc.wantStat)
			}
		})
	}
}

// --- runDoctor ---

func TestRunDoctor_allPass(t *testing.T) {
	goodCfg := &config.Config{
		TicketPrefix: "nc",
		ProjectRoot:  "/tmp/proj",
		GithubRepo:   "owner/repo",
		Agents:       config.AgentsConfig{Mayor: config.AgentConfig{Model: "claude-opus-4-6"}},
	}
	deps := doctorDeps{
		runCmd: func(name string, args ...string) (string, error) {
			switch name {
			case "dolt":
				return "dolt version 1.50.1", nil
			case "tmux":
				return "tmux 3.4", nil
			case "gh":
				return "gh version 2.0.0", nil
			case "git":
				return "git version 2.47.0", nil
			}
			return "", nil
		},
		findRoot:      func() (string, error) { return "/tmp/proj", nil },
		loadConfig:    func(root string) (*config.Config, error) { return goodCfg, nil },
		sessionExists: func(name string) bool { return true },
	}
	results, anyFail := runDoctor(deps)
	if anyFail {
		t.Error("expected no failures")
	}
	if len(results) == 0 {
		t.Error("expected results")
	}
}

func TestRunDoctor_oneFail(t *testing.T) {
	goodCfg := &config.Config{
		TicketPrefix: "nc",
		ProjectRoot:  "/tmp/proj",
		GithubRepo:   "owner/repo",
		Agents:       config.AgentsConfig{Mayor: config.AgentConfig{Model: "claude-opus-4-6"}},
	}
	deps := doctorDeps{
		runCmd: func(name string, args ...string) (string, error) {
			if name == "dolt" {
				return "", errors.New("not found")
			}
			switch name {
			case "tmux":
				return "tmux 3.4", nil
			case "gh":
				return "gh version 2.0.0", nil
			case "git":
				return "git version 2.47.0", nil
			}
			return "", nil
		},
		findRoot:      func() (string, error) { return "/tmp/proj", nil },
		loadConfig:    func(root string) (*config.Config, error) { return goodCfg, nil },
		sessionExists: func(name string) bool { return true },
	}
	_, anyFail := runDoctor(deps)
	if !anyFail {
		t.Error("expected a failure")
	}
}
