// Package gitlab implements the vcs.Provider interface using the glab CLI
// (https://gitlab.com/gitlab-org/cli). It is a thin shell over glab with
// JSON translation to match the GitHub-shaped data that Company Town consumers
// expect. Tested against glab 1.43.2 (see version.go).
//
// # Semantic differences from the GitHub adapter
//
// 1. request-changes is approximated by an unresolved MR note. GitLab has no
// first-class "request-changes" review state. The body is posted with
// glab mr note create --unique=false so repeat "needs work" comments surface
// even when the same reviewer pushes back twice. The actual block-on-merge
// effect depends on the GitLab project having "all threads must be resolved"
// enabled; the adapter does not enforce or report this setting.
//
// 2. Approvals do not carry a body. GitLab's approve action takes no message.
// If the caller passes a non-empty body to Approve, the adapter posts it as a
// separate glab mr note after the approval.
//
// 3. Subgroup project paths are valid. "kate/sub/myproj" is a real GitLab
// path and is passed verbatim to glab -R, which handles the slashes natively.
//
// 4. [changes-requested] note convention. GetReviewCommentsRaw synthesises
// review records from approvals (approved_by) and MR notes. Notes whose body
// starts with "[changes-requested]" — or with "[ct-reviewer][changes-requested]"
// — are reported as CHANGES_REQUESTED reviews; all other notes are COMMENTED.
// The canonical format for AI reviewer request-changes comments on GitLab is:
//
//	[ct-reviewer][changes-requested] Changes requested. ...
//
// The [ct-reviewer] prefix must come first so the daemon's human-feedback
// detection (which checks HasPrefix "[ct-reviewer]") skips it. The
// [changes-requested] sentinel must follow immediately so GetReviewCommentsRaw
// can classify the note state correctly.
//
// 5. Locked MR state. GitLab "locked" MRs are reported as OPEN because
// they are still effectively in-flight from Company Town's perspective. A
// debug note is logged; if CT ever needs to distinguish locked MRs a future
// ticket should expand the vcs.PRState.State enum rather than re-parsing glab
// JSON to recover this information.
//
// # Testdata goldens
//
// testdata/*.json files were captured against glab 1.43.2 on 2026-04-16.
// Re-capture is a deliberate decision tied to a glab version bump; do not
// update goldens without also bumping TestedAgainst in version.go.
package gitlab
