package commands

import (
	"bufio"
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
)

//go:embed templates/*
var templateFS embed.FS

// agentTypes defines the folder structure under .company_town/agents/
var agentTypes = []string{
	"mayor",
	"architect",
	"reviewer",
	"prole",
}

var artisanTypes = []string{
	"frontend",
	"backend",
	"qa_coder",
}

// directories under .company_town/ (besides agents/)
var topDirs = []string{
	"logs",
	"docs",
	"skills",
	"proles",
	"ticket_specs",
	"agents",
}

// gitRemoteURLFn is injectable for tests.
var gitRemoteURLFn = func() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitExecFn is injectable for tests — runs a git command in dir.
var gitExecFn = func(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

// gitRemoteGetURLInDirFn is injectable for tests — returns the origin URL for dir.
var gitRemoteGetURLInDirFn = func(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitLsRemoteMainFn is injectable for tests — runs `git ls-remote --heads origin main`
// in dir and returns stdout. An error means the remote is unreachable or invalid.
var gitLsRemoteMainFn = func(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "ls-remote", "--heads", "origin", "main").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// checkRemoteMain tests whether origin has a main branch. Returns a note to
// display (or "" if none needed) and whether `ct start` is the right next step.
// When platform or repoRef is unset, origin was not configured — no check needed.
func checkRemoteMain(projectRoot, platform, repoRef string) (note string, showNextStep bool) {
	if platform == "" || repoRef == "" {
		return "", true
	}
	out, err := gitLsRemoteMainFn(projectRoot)
	if err != nil {
		return "Note: Could not reach 'origin' to verify setup. When the remote exists and has a main branch, 'ct start' will succeed.", true
	}
	if out == "" {
		return "Note: 'origin' has no 'main' branch yet. Before running 'ct start', create an initial commit and push:\n\n  git commit --allow-empty -m \"initial commit\" && git push -u origin main", false
	}
	return "", true
}

// buildOriginURL constructs an HTTPS clone URL from the platform and repoRef.
func buildOriginURL(platform, repoRef string) string {
	switch platform {
	case "github":
		return "https://github.com/" + repoRef + ".git"
	case "gitlab":
		return "https://gitlab.com/" + repoRef + ".git"
	}
	return ""
}

// ensureGitOrigin checks the git state in projectRoot and sets up git + origin
// when needed. It is a no-op when origin is already configured. All three states
// are handled:
//   - no .git/      → git init + git remote add origin
//   - .git, no origin → git remote add origin
//   - .git + origin  → no-op
func ensureGitOrigin(projectRoot, platform, repoRef string) error {
	if platform == "" || repoRef == "" {
		return nil
	}
	originURL := buildOriginURL(platform, repoRef)
	if originURL == "" {
		return nil
	}

	gitDir := filepath.Join(projectRoot, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		if err := gitExecFn(projectRoot, "init"); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
		fmt.Println("  git init: initialized repository")
		if err := gitExecFn(projectRoot, "remote", "add", "origin", originURL); err != nil {
			return fmt.Errorf("git remote add origin: %w", err)
		}
		fmt.Printf("  git remote add origin %s\n", originURL)
		fmt.Println("  (to use SSH instead: git remote set-url origin git@<host>:<owner>/<repo>.git)")
		return nil
	}

	if _, err := gitRemoteGetURLInDirFn(projectRoot); err != nil {
		if err := gitExecFn(projectRoot, "remote", "add", "origin", originURL); err != nil {
			return fmt.Errorf("git remote add origin: %w", err)
		}
		fmt.Printf("  git remote add origin %s\n", originURL)
		fmt.Println("  (to use SSH instead: git remote set-url origin git@<host>:<owner>/<repo>.git)")
		return nil
	}

	fmt.Println("  exists:  git remote origin (unchanged)")
	return nil
}

// deriveTicketPrefix extracts a lowercase alpha prefix from the project directory name.
// Returns "" if nothing usable can be extracted.
func deriveTicketPrefix(projectRoot string) string {
	name := strings.ToLower(filepath.Base(projectRoot))
	var b strings.Builder
	for _, c := range name {
		if c >= 'a' && c <= 'z' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// deriveSessionPrefix produces a tmux session prefix from the project directory name.
// Non-alphanumeric chars become hyphens; the result is lowercased and gets a
// trailing hyphen appended. Falls back to "ct-" if nothing usable can be extracted.
func deriveSessionPrefix(projectRoot string) string {
	name := strings.ToLower(filepath.Base(projectRoot))
	var b strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "ct-"
	}
	return result + "-"
}

// deriveDoltDatabase produces a MySQL-legal identifier from the project directory name.
// Non-alphanumeric chars become underscores; leading digits are stripped.
// Falls back to "company_town" if nothing usable remains.
func deriveDoltDatabase(projectRoot string) string {
	name := strings.ToLower(filepath.Base(projectRoot))
	var b strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	result := strings.Trim(b.String(), "_")
	for len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = result[1:]
	}
	if result == "" {
		return "company_town"
	}
	return result
}

// deriveVCSPlatformFromURL returns "github", "gitlab", or "" based on the
// host embedded in the remote URL.
func deriveVCSPlatformFromURL(raw string) string {
	switch {
	case strings.Contains(raw, "github.com"):
		return "github"
	case strings.Contains(raw, "gitlab.com"):
		return "gitlab"
	default:
		return ""
	}
}

// deriveRepoRefFromURL extracts the "owner/repo" (or "namespace/project")
// portion from a GitHub or GitLab remote URL in either HTTPS or SSH form.
// Returns "" if the URL is not recognised.
func deriveRepoRefFromURL(raw string) string {
	raw = strings.TrimSuffix(raw, ".git")
	for _, sep := range []string{"github.com/", "gitlab.com/", "github.com:", "gitlab.com:"} {
		if idx := strings.Index(raw, sep); idx >= 0 {
			return raw[idx+len(sep):]
		}
	}
	return ""
}

// deriveRepoRef attempts to parse the git origin URL into "owner/repo" form.
// Returns "" when the origin is unavailable or cannot be parsed.
func deriveRepoRef() string {
	raw, err := gitRemoteURLFn()
	if err != nil || raw == "" {
		return ""
	}
	return deriveRepoRefFromURL(raw)
}

// validatePlatform returns an error if s is not a recognised VCS platform.
func validatePlatform(s string) error {
	switch s {
	case "github", "gitlab":
		return nil
	}
	return fmt.Errorf("must be %q or %q", "github", "gitlab")
}

// validateTicketPrefix returns an error if s is not a valid ticket prefix.
func validateTicketPrefix(s string) error {
	if s == "" {
		return fmt.Errorf("ticket_prefix cannot be empty")
	}
	for _, c := range s {
		if c < 'a' || c > 'z' {
			return fmt.Errorf("ticket_prefix must contain only lowercase letters a-z (got %q)", s)
		}
	}
	return nil
}

// promptField reads one value from r, showing label and (if non-empty) defaultVal
// in brackets. An empty response accepts the default. validate is retried on failure.
func promptField(r *bufio.Reader, label, defaultVal string, validate func(string) error) (string, error) {
	for {
		if defaultVal != "" {
			fmt.Printf("  %s [%s]: ", label, defaultVal)
		} else {
			fmt.Printf("  %s: ", label)
		}
		line, err := r.ReadString('\n')
		val := strings.TrimSpace(line)
		if val == "" {
			val = defaultVal
		}
		if err != nil {
			if err == io.EOF {
				// Treat EOF as accepting the current value (supports piped input).
				if validate != nil {
					if verr := validate(val); verr != nil {
						return "", fmt.Errorf("invalid value for %s: %w", label, verr)
					}
				}
				fmt.Println(val)
				return val, nil
			}
			return "", fmt.Errorf("reading %s: %w", label, err)
		}
		if validate != nil {
			if verr := validate(val); verr != nil {
				fmt.Printf("  error: %v\n", verr)
				continue
			}
		}
		return val, nil
	}
}

// initParams holds the user-supplied (or derived) configuration values for a
// new project.
type initParams struct {
	platform       string // "github" or "gitlab"
	repoRef        string // owner/repo or namespace/project
	ticketPrefix   string
	doltDatabase   string
	doltPort       int
	sessionPrefix  string
	languagePreset string // "go", "python", or "" for agnostic-only
}

// validateLanguagePreset returns an error if s is not a recognised preset name.
func validateLanguagePreset(s string) error {
	switch s {
	case "go", "python", "":
		return nil
	}
	return fmt.Errorf("must be %q, %q, or blank for agnostic-only", "go", "python")
}

// collectInitParams gathers user-configurable fields either interactively
// (prompting via r) or by applying derived defaults when nonInteractive is true.
func collectInitParams(nonInteractive bool, r io.Reader, projectRoot string, defaultPort int) (initParams, error) {
	prefix := deriveTicketPrefix(projectRoot)
	dbName := deriveDoltDatabase(projectRoot)
	sesPrefix := deriveSessionPrefix(projectRoot)
	lang := config.DetectLanguagePreset(projectRoot)

	// Derive platform and repo ref from the git remote URL in one call.
	rawURL, urlErr := gitRemoteURLFn()
	var detectedPlatform, detectedRepoRef string
	if urlErr == nil && rawURL != "" {
		detectedPlatform = deriveVCSPlatformFromURL(rawURL)
		detectedRepoRef = deriveRepoRefFromURL(rawURL)
	}

	if nonInteractive {
		if prefix == "" {
			prefix = "tk"
		}
		return initParams{
			platform:       detectedPlatform, // empty if detection failed — caller must edit config.json
			repoRef:        detectedRepoRef,  // empty if detection failed
			ticketPrefix:   prefix,
			doltDatabase:   dbName,
			doltPort:       defaultPort,
			sessionPrefix:  sesPrefix,
			languagePreset: lang,
		}, nil
	}

	fmt.Println("Configure your Company Town project (press Enter to accept the default):")
	br := bufio.NewReader(r)

	tp, err := promptField(br, "Ticket prefix (lowercase letters, e.g. nc)", prefix, validateTicketPrefix)
	if err != nil {
		return initParams{}, fmt.Errorf("ticket_prefix: %w", err)
	}

	plat, err := promptField(br, "VCS platform (github, gitlab)", detectedPlatform, validatePlatform)
	if err != nil {
		return initParams{}, fmt.Errorf("platform: %w", err)
	}

	repoLabel := "GitHub repo (owner/repo)"
	if plat == "gitlab" {
		repoLabel = "GitLab project (namespace/project)"
	}
	rr, err := promptField(br, repoLabel, detectedRepoRef, nil)
	if err != nil {
		return initParams{}, fmt.Errorf("repo: %w", err)
	}

	dd, err := promptField(br, "Dolt database name", dbName, nil)
	if err != nil {
		return initParams{}, fmt.Errorf("dolt.database: %w", err)
	}

	ps, err := promptField(br, "Dolt port", strconv.Itoa(defaultPort), nil)
	if err != nil {
		return initParams{}, fmt.Errorf("dolt.port: %w", err)
	}
	port, err := strconv.Atoi(ps)
	if err != nil || port < 1 || port > 65535 {
		fmt.Printf("  invalid port %q, using %d\n", ps, defaultPort)
		port = defaultPort
	}

	sp, err := promptField(br, "Tmux session prefix (e.g. myproject-)", sesPrefix, nil)
	if err != nil {
		return initParams{}, fmt.Errorf("session_prefix: %w", err)
	}

	lp, err := promptField(br, "Language preset (go, python, or blank for agnostic-only)", lang, validateLanguagePreset)
	if err != nil {
		return initParams{}, fmt.Errorf("language_preset: %w", err)
	}

	return initParams{
		platform:       plat,
		repoRef:        rr,
		ticketPrefix:   tp,
		doltDatabase:   dd,
		doltPort:       port,
		sessionPrefix:  sp,
		languagePreset: lp,
	}, nil
}

// Init implements `ct init`. args may contain --non-interactive to skip prompts.
func Init(args []string) error {
	nonInteractive := false
	for _, a := range args {
		if a == "--non-interactive" {
			nonInteractive = true
		}
	}
	return initCore(nonInteractive, os.Stdin)
}

func initCore(nonInteractive bool, stdin io.Reader) error {
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	ctDir := config.CompanyTownDir(projectRoot)
	fmt.Printf("Initializing company town in %s\n", ctDir)

	// 1. Create top-level directories
	for _, d := range topDirs {
		dir := filepath.Join(ctDir, d)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}

	// 2. Create agent directories with memory/
	for _, agent := range agentTypes {
		agentDir := filepath.Join(ctDir, "agents", agent)
		memDir := filepath.Join(agentDir, "memory")
		if err := os.MkdirAll(memDir, 0755); err != nil {
			return fmt.Errorf("creating agent dir %s: %w", agent, err)
		}
		WriteClaudeMD(agentDir, agent)
	}

	// 3. Create artisan subdirectories
	artisanBase := filepath.Join(ctDir, "agents", "artisan")
	if err := os.MkdirAll(artisanBase, 0755); err != nil {
		return fmt.Errorf("creating artisan base: %w", err)
	}
	WriteClaudeMD(artisanBase, "artisan")

	for _, specialty := range artisanTypes {
		specDir := filepath.Join(artisanBase, specialty)
		memDir := filepath.Join(specDir, "memory")
		if err := os.MkdirAll(memDir, 0755); err != nil {
			return fmt.Errorf("creating artisan/%s: %w", specialty, err)
		}
		WriteClaudeMD(specDir, "artisan-"+specialty)
	}

	// 4. Write config.json if missing, prompting for key fields.
	cfgPath := config.ConfigPath(projectRoot)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		defaultPort, portErr := pickFreePort(3307)
		if portErr != nil {
			return fmt.Errorf("finding free dolt port: %w", portErr)
		}

		params, err := collectInitParams(nonInteractive, stdin, projectRoot, defaultPort)
		if err != nil {
			return fmt.Errorf("collecting init params: %w", err)
		}

		cfg := config.DefaultConfig(projectRoot, params.platform, params.repoRef)
		cfg.TicketPrefix = params.ticketPrefix
		cfg.SessionPrefix = params.sessionPrefix
		cfg.Dolt.Port = params.doltPort
		cfg.Dolt.Database = params.doltDatabase
		cfg.Language = params.languagePreset
		cfg.Quality.Checks = config.QualityChecksForPreset(params.languagePreset)
		if err := config.Write(projectRoot, cfg); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		presetLabel := params.languagePreset
		if presetLabel == "" {
			presetLabel = "agnostic"
		}
		fmt.Printf("  created: config.json (platform=%s, ticket_prefix=%q, session_prefix=%q, dolt port=%d, database=%q, preset=%s)\n",
			params.platform, params.ticketPrefix, params.sessionPrefix, params.doltPort, params.doltDatabase, presetLabel)
	} else {
		fmt.Println("  exists:  config.json")
	}

	// 5. Load config for Dolt settings
	cfg, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// 5a. Ensure git repo + origin are set up (idempotent).
	if err := ensureGitOrigin(projectRoot, cfg.Platform, cfg.Repo); err != nil {
		return fmt.Errorf("setting up git origin: %w", err)
	}

	// 6. Connect to Dolt server (start it if not running)
	fmt.Println("Connecting to Dolt server...")
	conn, err := db.Connect(&cfg.Dolt)
	if err != nil {
		fmt.Println("  Dolt server not responding, starting it...")
		doltDir := filepath.Join(ctDir, "db")
		if err := db.InitDolt(doltDir); err != nil {
			return fmt.Errorf("dolt init: %w", err)
		}
		if err := db.StartServer(doltDir, ctDir, &cfg.Dolt); err != nil {
			return fmt.Errorf("starting dolt server: %w", err)
		}
		conn, err = db.Connect(&cfg.Dolt)
		if err != nil {
			return fmt.Errorf("connecting to dolt after start: %w", err)
		}
	}
	defer conn.Close()

	fmt.Println("Running migrations...")
	if err := db.RunMigrations(conn); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	// 7. Write .gitignore inside .company_town (ignores its own contents).
	gitignorePath := filepath.Join(ctDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		content := "# Everything in .company_town is local runtime state\n*\n"
		if err := os.WriteFile(gitignorePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing .gitignore: %w", err)
		}
	}

	// 8. Ensure .company_town/ is excluded from the project root .gitignore so
	// it is never accidentally committed. Append only if not already present.
	if err := ensureRootGitignore(projectRoot); err != nil {
		return fmt.Errorf("updating root .gitignore: %w", err)
	}

	fmt.Println("\nCompany Town initialized.")
	note, hasMain := checkRemoteMain(projectRoot, cfg.Platform, cfg.Repo)
	if note != "" {
		fmt.Println()
		fmt.Println(note)
	}
	if hasMain {
		fmt.Println("Next: run `ct start`")
	}
	return nil
}

// matchesCompanyTownEntry reports whether a .gitignore line already excludes
// .company_town/ in any of the four equivalent forms:
//
//	.company_town/       (canonical)
//	.company_town        (no trailing slash)
//	/.company_town/      (root-anchored)
//	/.company_town       (root-anchored, no trailing slash)
//
// Inline comments (# …) and surrounding whitespace are stripped before
// comparison, so ".company_town/  # local runtime state" also matches.
func matchesCompanyTownEntry(line string) bool {
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "/")
	line = strings.TrimSuffix(line, "/")
	return line == ".company_town"
}

// ensureRootGitignore adds a .company_town/ exclusion to the project root's
// .gitignore. If the file does not exist it is created. If the entry is already
// present the file is left unchanged.
func ensureRootGitignore(projectRoot string) error {
	const entry = ".company_town/"
	path := filepath.Join(projectRoot, ".gitignore")

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading root .gitignore: %w", err)
	}

	// Check each line; recognise all equivalent forms so we don't duplicate.
	for _, line := range strings.Split(string(existing), "\n") {
		if matchesCompanyTownEntry(line) {
			fmt.Println("  exists:  .gitignore (.company_town/ already excluded)")
			return nil
		}
	}

	// Append with a trailing newline; prepend a blank line if the file is
	// non-empty and does not already end with one.
	suffix := entry + "\n"
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		suffix = "\n" + suffix
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening root .gitignore: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(suffix); err != nil {
		return fmt.Errorf("writing root .gitignore: %w", err)
	}

	if len(existing) == 0 {
		fmt.Println("  created: .gitignore (excludes .company_town/)")
	} else {
		fmt.Println("  updated: .gitignore (added .company_town/)")
	}
	return nil
}

// WriteClaudeMD writes a CLAUDE.md for an agent type from the embedded templates.
// Always overwrites any existing file.
func WriteClaudeMD(dir, agentType string) {
	path := filepath.Join(dir, "CLAUDE.md")

	content, err := LoadTemplate(agentType)
	if err != nil {
		fmt.Printf("  error: no template for %s: %v\n", agentType, err)
		return
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Printf("  error writing %s: %v\n", path, err)
		return
	}
	fmt.Printf("  created: agents/%s/CLAUDE.md\n", agentType)
}

// LoadTemplate reads a template file from the embedded filesystem
// and appends the shared commands reference.
func LoadTemplate(agentType string) (string, error) {
	filename := fmt.Sprintf("templates/%s-CLAUDE.md", agentType)
	data, err := templateFS.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("reading template %s: %w", filename, err)
	}

	content := string(data)

	// Artisan specialty files inherit from base — don't append commands ref
	if strings.HasPrefix(agentType, "artisan-") {
		return content, nil
	}

	// Append shared commands reference
	ref, err := templateFS.ReadFile("templates/commands-reference.md")
	if err != nil {
		return content, nil //nolint:nilerr // non-fatal: commands reference template is optional
	}

	return content + "\n" + string(ref), nil
}
