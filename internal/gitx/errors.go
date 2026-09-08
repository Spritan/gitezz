package gitx

import (
	"strings"
)

// ConflictKind classifies actionable git failures for UI dialogs.
type ConflictKind string

const (
	KindNone          ConflictKind = ""
	KindSwitchDirty   ConflictKind = "switch_dirty"
	KindPullConflict  ConflictKind = "pull_conflict"
	KindPullOverwrite ConflictKind = "pull_overwrite"
	KindPushNonFF     ConflictKind = "push_non_ff"
	KindPushAuth      ConflictKind = "push_auth"
	KindCommitEmpty   ConflictKind = "commit_empty"
	KindStashEmpty    ConflictKind = "stash_empty"
	KindGeneric       ConflictKind = "generic"
)

// Op names passed to Classify.
const (
	OpSwitch = "switch"
	OpPull   = "pull"
	OpPush   = "push"
	OpCommit = "commit"
	OpStash  = "stash"
	OpFetch  = "fetch"
)

// Classify maps git stderr/stdout to a conflict kind for the given operation.
func Classify(op, out string) ConflictKind {
	lower := strings.ToLower(out)

	switch op {
	case OpSwitch:
		if containsAny(lower, "would be overwritten", "your local changes",
			"please commit your changes", "please commit or stash") {
			return KindSwitchDirty
		}
	case OpPull:
		if containsAny(lower, "conflict", "fix conflicts", "unmerged paths", "merge conflict") {
			return KindPullConflict
		}
		if containsAny(lower, "would be overwritten by merge", "would be overwritten by checkout",
			"your local changes to the following files would be overwritten",
			"please commit your changes or stash") {
			return KindPullOverwrite
		}
		if containsAny(lower, "authentication failed", "permission denied",
			"could not read from remote", "publickey", "access denied",
			"could not resolve hostname", "repository not found", "invalid username or token") {
			return KindPushAuth
		}
		if containsAny(lower, "cannot fast-forward", "diverged", "non-fast-forward") {
			return KindPullConflict
		}
	case OpPush:
		if containsAny(lower, "non-fast-forward", "fetch first", "[rejected]",
			"updates were rejected", "failed to push some refs") {
			return KindPushNonFF
		}
		if containsAny(lower, "authentication failed", "permission denied",
			"could not read from remote", "publickey", "access denied",
			"could not resolve hostname", "repository not found") {
			return KindPushAuth
		}
	case OpFetch:
		if containsAny(lower, "authentication failed", "permission denied",
			"could not read from remote", "publickey", "access denied",
			"could not resolve hostname", "repository not found") {
			return KindPushAuth
		}
	case OpCommit:
		if containsAny(lower, "nothing to commit", "no changes added to commit",
			"nothing added to commit") {
			return KindCommitEmpty
		}
	case OpStash:
		if containsAny(lower, "no local changes to save") {
			return KindStashEmpty
		}
	}

	if out != "" {
		return KindGeneric
	}
	return KindNone
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// Title returns a short dialog title for the kind.
func (k ConflictKind) Title() string {
	switch k {
	case KindSwitchDirty:
		return "Switch blocked"
	case KindPullConflict:
		return "Pull conflicts"
	case KindPullOverwrite:
		return "Pull blocked"
	case KindPushNonFF:
		return "Push rejected"
	case KindPushAuth:
		return "Remote access failed"
	case KindCommitEmpty:
		return "Nothing to commit"
	case KindStashEmpty:
		return "Nothing to stash"
	default:
		return "Git error"
	}
}

// StatusLabel is a short row status string.
func (k ConflictKind) StatusLabel() string {
	switch k {
	case KindSwitchDirty:
		return "Blocked · dirty"
	case KindPullConflict:
		return "Conflict"
	case KindPullOverwrite:
		return "Needs attention"
	case KindPushNonFF:
		return "Push rejected"
	case KindPushAuth:
		return "Auth failed"
	case KindCommitEmpty:
		return "Nothing to commit"
	case KindStashEmpty:
		return "Clean"
	default:
		return "Failed"
	}
}

// Actionable reports whether the UI should offer recovery choices.
func (k ConflictKind) Actionable() bool {
	switch k {
	case KindSwitchDirty, KindPullConflict, KindPullOverwrite, KindPushNonFF, KindPushAuth, KindCommitEmpty:
		return true
	default:
		return false
	}
}
