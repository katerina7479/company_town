package commands

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/eventlog"
)

// Metrics implements `ct metrics [--since N]`.
func Metrics(args []string) error {
	since := 7
	for i, a := range args {
		if a == "--since" && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				since = n
			}
		}
	}
	sinceTime := time.Now().Add(-time.Duration(since) * 24 * time.Hour)

	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	ctDir := config.CompanyTownDir(projectRoot)
	logPath := filepath.Join(ctDir, "logs", "events.jsonl")

	reader := eventlog.NewReader(logPath)
	events, err := reader.ReadFiltered(eventlog.Filter{Since: sinceTime})
	if err != nil {
		return fmt.Errorf("reading events: %w", err)
	}

	if len(events) == 0 {
		fmt.Println("No events found in the specified time window.")
		return nil
	}

	printMetricsHeader(since)
	printTicketFlow(events)
	printAgentUtilization(events, sinceTime)
	printPRCycle(events)

	return nil
}

func printMetricsHeader(days int) {
	title := lipgloss.NewStyle().Bold(true).Render(
		fmt.Sprintf("Company Town Metrics (last %d days)", days),
	)
	separator := strings.Repeat("═", 40)
	fmt.Printf("\n%s\n%s\n\n", title, separator)
}

type timedTransition struct {
	from string
	to   string
	at   time.Time
}

type timedAgentTransition struct {
	from string
	to   string
	at   time.Time
}

func avgDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return total / time.Duration(len(durations))
}

func printTicketFlow(events []eventlog.Event) {
	sectionStyle := lipgloss.NewStyle().Bold(true)
	fmt.Println(sectionStyle.Render("Ticket Flow"))

	// Group ticket_status_changed events by entity_id (ticket ID as string)
	byTicket := map[string][]timedTransition{}
	for _, evt := range events {
		if evt.Kind != eventlog.KindTicketStatusChanged {
			continue
		}
		byTicket[evt.EntityID] = append(byTicket[evt.EntityID], timedTransition{
			from: evt.FromStatus,
			to:   evt.ToStatus,
			at:   evt.Timestamp,
		})
	}

	// Sort each ticket's transitions by time
	for id := range byTicket {
		sort.Slice(byTicket[id], func(i, j int) bool {
			return byTicket[id][i].at.Before(byTicket[id][j].at)
		})
	}

	// Throughput: count transitions to "closed"
	closed := 0
	for _, transitions := range byTicket {
		for _, t := range transitions {
			if t.to == "closed" {
				closed++
			}
		}
	}
	fmt.Printf("  Throughput:        %d tickets closed\n", closed)

	var closeTimes []time.Duration
	repairCount := 0
	reviewCount := 0
	timeInStatus := map[string][]time.Duration{}

	for _, transitions := range byTicket {
		var firstOpen, lastClose time.Time
		hadReview := false
		hadRepair := false

		for i, t := range transitions {
			if t.to == "open" && firstOpen.IsZero() {
				firstOpen = t.at
			}
			if t.to == "closed" {
				lastClose = t.at
			}
			if t.to == "in_review" {
				hadReview = true
			}
			if t.to == "repairing" {
				hadRepair = true
			}
			// Time in status: duration from this transition to the next
			if i+1 < len(transitions) {
				dur := transitions[i+1].at.Sub(t.at)
				timeInStatus[t.to] = append(timeInStatus[t.to], dur)
			}
		}

		if !firstOpen.IsZero() && !lastClose.IsZero() {
			closeTimes = append(closeTimes, lastClose.Sub(firstOpen))
		}
		if hadReview {
			reviewCount++
		}
		if hadRepair {
			repairCount++
		}
	}

	if len(closeTimes) > 0 {
		fmt.Printf("  Avg time to close: %s\n", formatDuration(avgDuration(closeTimes)))
	}

	if reviewCount > 0 {
		rate := float64(repairCount) / float64(reviewCount) * 100
		fmt.Printf("  Repair rate:       %.0f%% (%d/%d sent back)\n", rate, repairCount, reviewCount)
	}

	if len(timeInStatus) > 0 {
		fmt.Println("\n  Avg time in status:")
		statusOrder := []string{"draft", "open", "in_progress", "in_review", "under_review", "pr_open", "reviewed", "repairing"}
		for _, s := range statusOrder {
			if durations, ok := timeInStatus[s]; ok {
				fmt.Printf("    %-14s → %s\n", s, formatDuration(avgDuration(durations)))
			}
		}
	}
	fmt.Println()
}

func printAgentUtilization(events []eventlog.Event, since time.Time) {
	sectionStyle := lipgloss.NewStyle().Bold(true)
	fmt.Println(sectionStyle.Render("Agent Utilization"))

	byAgent := map[string][]timedAgentTransition{}
	for _, evt := range events {
		if evt.Kind != eventlog.KindAgentStatusChanged {
			continue
		}
		byAgent[evt.EntityID] = append(byAgent[evt.EntityID], timedAgentTransition{
			from: evt.FromStatus,
			to:   evt.ToStatus,
			at:   evt.Timestamp,
		})
	}

	for name := range byAgent {
		sort.Slice(byAgent[name], func(i, j int) bool {
			return byAgent[name][i].at.Before(byAgent[name][j].at)
		})
	}

	now := time.Now()
	var agentNames []string
	for name := range byAgent {
		agentNames = append(agentNames, name)
	}
	sort.Strings(agentNames)

	for _, name := range agentNames {
		transitions := byAgent[name]
		totalTime := now.Sub(since)
		var workingTime time.Duration

		for i, t := range transitions {
			if t.to == "working" {
				end := now
				if i+1 < len(transitions) {
					end = transitions[i+1].at
				}
				workingTime += end.Sub(t.at)
			}
		}

		utilPct := float64(workingTime) / float64(totalTime) * 100
		fmt.Printf("  %-12s %3.0f%% working\n", name, utilPct)
	}
	fmt.Println()
}

func printPRCycle(events []eventlog.Event) {
	sectionStyle := lipgloss.NewStyle().Bold(true)
	fmt.Println(sectionStyle.Render("PR Cycle"))

	byTicket := map[string][]timedTransition{}
	for _, evt := range events {
		if evt.Kind != eventlog.KindTicketStatusChanged {
			continue
		}
		byTicket[evt.EntityID] = append(byTicket[evt.EntityID], timedTransition{
			from: evt.FromStatus,
			to:   evt.ToStatus,
			at:   evt.Timestamp,
		})
	}

	for id := range byTicket {
		sort.Slice(byTicket[id], func(i, j int) bool {
			return byTicket[id][i].at.Before(byTicket[id][j].at)
		})
	}

	var cycleTimes []time.Duration
	var reviewRounds []int

	for _, transitions := range byTicket {
		var prOpen time.Time
		rounds := 0

		for _, t := range transitions {
			if t.to == "pr_open" && prOpen.IsZero() {
				prOpen = t.at
			}
			if t.to == "in_review" {
				rounds++
			}
			if t.to == "closed" && !prOpen.IsZero() {
				cycleTimes = append(cycleTimes, t.at.Sub(prOpen))
			}
		}

		if rounds > 0 {
			reviewRounds = append(reviewRounds, rounds)
		}
	}

	if len(cycleTimes) > 0 {
		fmt.Printf("  Avg PR cycle:      %s\n", formatDuration(avgDuration(cycleTimes)))
	}
	if len(reviewRounds) > 0 {
		total := 0
		for _, r := range reviewRounds {
			total += r
		}
		avg := float64(total) / float64(len(reviewRounds))
		fmt.Printf("  Avg review rounds: %.1f\n", avg)
	}
	fmt.Println()
}
