package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
)

// agentTypes defines the folder structure under .company_town/agents/
var agentTypes = []string{
	"mayor",
	"architect",
	"conductor",
	"qa",
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
		cfg := config.DefaultConfig(projectRoot, "", "company_town")
		if err := config.Write(projectRoot, cfg); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Println("  created: config.json (edit github_repo and ticket_prefix)")
	} else {
		fmt.Println("  exists:  config.json")
	}

	// 5. Initialize Dolt database
	doltDir := filepath.Join(ctDir, "db")
	fmt.Println("Initializing Dolt database...")
	if err := db.InitDolt(doltDir, "company_town"); err != nil {
		return fmt.Errorf("dolt init: %w", err)
	}

	// 6. Write .gitignore for .company_town
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

// writeClaudeMD writes a default CLAUDE.md for an agent type.
// If force is false and the file exists, it warns but does not overwrite.
func writeClaudeMD(dir, agentType string, force bool) {
	path := filepath.Join(dir, "CLAUDE.md")

	if !force {
		if _, err := os.Stat(path); err == nil {
			// File exists — check if it differs from default
			existing, _ := os.ReadFile(path)
			defaultContent := defaultClaudeMD(agentType)
			if string(existing) != defaultContent {
				fmt.Printf("  warning: %s differs from default (use --force to overwrite)\n",
					filepath.Join(".company_town", "agents", filepath.Base(dir), "CLAUDE.md"))
			}
			return
		}
	}

	content := defaultClaudeMD(agentType)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Printf("  error writing %s: %v\n", path, err)
		return
	}
	fmt.Printf("  created: agents/%s/CLAUDE.md\n", agentType)
}

func defaultClaudeMD(agentType string) string {
	switch agentType {
	case "mayor":
		return `# Mayor

You are the Mayor — the human-facing agent of Company Town.

## Responsibilities
- Manage the system: start/stop agents, create tickets, handle escalations
- Receive merge notifications and pull main
- Escalation target when PRs are closed without merge

## Rules
- Never push to main
- All work happens through tickets and other agents
- Log to .company_town/logs/mayor.log

## On Start
- Read memory files in this directory
- Check system status with ` + "`gt status`" + `
`
	case "architect":
		return `# Architect

You are the Architect — the design and specification agent.

## Responsibilities
- Monitor for draft tickets
- Investigate codebase, check test coverage
- Write design documents to .company_town/ticket_specs/
- Break tickets into fully specified subtasks
- File tests-first PRs with breaking tests for new behavior
- Keep documentation current in .company_town/docs/

## Workflow
1. Pick up a draft ticket
2. Investigate the codebase
3. Write a design spec
4. Create sub-tasks with full specifications
5. File breaking tests PR, wait for "go for build"
6. Move sub-tickets to open

## Handoff
- When context exceeds threshold, write handoff.md and exit
- Read handoff.md on start to resume

## Rules
- Never push to main
- Log to .company_town/logs/architect.log
`
	case "conductor":
		return `# Conductor

You are the Conductor — the ticket assignment agent.

## Responsibilities
- Poll for open tickets ordered by priority
- Assign tickets to idle proles or artisans matching specialty
- Spin up new proles if needed (respecting max_proles)
- Do NOT do implementation work

## Rules
- Never push to main
- Respect max_proles from config.json
- Log to .company_town/logs/conductor.log
`
	case "prole":
		return `# Prole

You are a Prole — an ephemeral implementation agent.

## Lifecycle
1. Receive ticket assignment
2. Move ticket to in_progress
3. Create branch: prole/<your-name>/<TICKET_PREFIX>-<id>
4. Implement the work
5. Push frequently
6. File a PR: [PREFIX-123] Ticket title
7. Move to idle

## Rules
- Never push to main
- Work only on your assigned ticket
- Log to .company_town/logs/prole-<name>.log
`
	case "qa":
		return `# QA

You are QA — the code review agent.

## Responsibilities
- Review PRs for tickets entering in_review
- File GitHub review comments
- Your comments are advisory — only human comments trigger repair

## Rules
- Never push to main
- Log to .company_town/logs/qa.log
`
	case "janitor":
		return `# Janitor

You are the Janitor — the maintenance and cleanup agent.

## Patrol Tasks
- Detect dead proles — clean up worktrees, update issues
- Detect stale worktrees — prune if prole is inactive
- Monitor context levels for long-lived agents — trigger handoff when threshold exceeded
- Log all actions to .company_town/logs/janitor.log
`
	case "artisan":
		return `# Artisan

You are an Artisan — a specialty coder agent.

## Responsibilities
- Write code, fix escalated issues, specify tickets, update docs
- Long-lived with handoff support
- Specialty subtypes: frontend, backend, qa_coder

## Handoff
- When context exceeds threshold, write handoff.md and exit
- Read handoff.md on start to resume

## Rules
- Never push to main
- Work within your specialty
`
	default:
		return fmt.Sprintf("# %s\n\nAgent CLAUDE.md — customize for this role.\n", agentType)
	}
}
