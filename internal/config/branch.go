package config

import "fmt"

// ProleBranchName returns the canonical git branch name for a prole working
// on a given ticket. Format: "prole/<name>/<prefix>-<id>", where prefix is the
// project's configured ticket prefix (case-sensitive, used verbatim).
//
// Example: ProleBranchName("nc", "copper", 56) → "prole/copper/nc-56"
func ProleBranchName(prefix, proleName string, ticketID int) string {
	return fmt.Sprintf("prole/%s/%s-%d", proleName, prefix, ticketID)
}
