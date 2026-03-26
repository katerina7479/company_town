package commands

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
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
	"conductor",
	"reviewer",
	"janitor",
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

// Init implements `ct init`.
func Init(force bool) error {
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
		writeClaudeMD(agentDir, agent, force)
	}

	// 3. Create artisan subdirectories
	artisanBase := filepath.Join(ctDir, "agents", "artisan")
	if err := os.MkdirAll(artisanBase, 0755); err != nil {
		return fmt.Errorf("creating artisan base: %w", err)
	}
	writeClaudeMD(artisanBase, "artisan", force)

	for _, specialty := range artisanTypes {
		specDir := filepath.Join(artisanBase, specialty)
		memDir := filepath.Join(specDir, "memory")
		if err := os.MkdirAll(memDir, 0755); err != nil {
			return fmt.Errorf("creating artisan/%s: %w", specialty, err)
		}
		writeClaudeMD(specDir, "artisan-"+specialty, force)
	}

	// 4. Write config.json if missing
	cfgPath := config.ConfigPath(projectRoot)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfg := config.DefaultConfig(projectRoot, "")
		if err := config.Write(projectRoot, cfg); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Println("  created: config.json (edit github_repo and ticket_prefix)")
	} else {
		fmt.Println("  exists:  config.json")
	}

	// 5. Load config for Dolt settings
	cfg, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
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

	// 7. Write .gitignore for .company_town
	gitignorePath := filepath.Join(ctDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		content := "# Everything in .company_town is local runtime state\n*\n"
		if err := os.WriteFile(gitignorePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing .gitignore: %w", err)
		}
	}

	fmt.Println("\nCompany Town initialized.")
	fmt.Println("Next: edit .company_town/config.json, then run `ct start`")
	return nil
}

// writeClaudeMD writes a CLAUDE.md for an agent type from the embedded templates.
// If force is false and the file exists, it warns but does not overwrite.
func writeClaudeMD(dir, agentType string, force bool) {
	path := filepath.Join(dir, "CLAUDE.md")

	content, err := loadTemplate(agentType)
	if err != nil {
		fmt.Printf("  error: no template for %s: %v\n", agentType, err)
		return
	}

	if !force {
		if _, err := os.Stat(path); err == nil {
			existing, _ := os.ReadFile(path)
			if string(existing) != content {
				fmt.Printf("  warning: %s differs from default (use --force to overwrite)\n",
					filepath.Join(".company_town", "agents", filepath.Base(dir), "CLAUDE.md"))
			}
			return
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Printf("  error writing %s: %v\n", path, err)
		return
	}
	fmt.Printf("  created: agents/%s/CLAUDE.md\n", agentType)
}

// loadTemplate reads a template file from the embedded filesystem
// and appends the shared commands reference.
func loadTemplate(agentType string) (string, error) {
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
		return content, nil // non-fatal if missing
	}

	return content + "\n" + string(ref), nil
}
