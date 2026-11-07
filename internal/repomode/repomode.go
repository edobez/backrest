// Package repomode decides which operations Backrest may perform on a repo,
// based on the repo's configured RepoMode.
//
// restic has no notion of repository ownership: any key can forget and prune.
// The repo mode is how a user tells Backrest that a repo is managed elsewhere.
// The rules live here so that config validation, the task layer, the repo
// orchestrator and the API all enforce the same thing.
package repomode

import (
	"errors"
	"fmt"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
)

// Action is an operation that a repo mode may forbid.
type Action int

const (
	ActionBackup Action = iota
	ActionForget
	ActionPrune
	ActionUnlock
	// ActionInit creates a repo that does not exist yet.
	ActionInit
	// ActionScheduledCheck is a check started by a schedule.
	ActionScheduledCheck
	// ActionManualCheck is a check explicitly started by the user.
	ActionManualCheck
	// ActionTag modifies snapshot tags. restic rewrites the snapshot and
	// removes the original, so this is a removal too.
	ActionTag
)

func (a Action) String() string {
	switch a {
	case ActionBackup:
		return "backup"
	case ActionForget:
		return "forget"
	case ActionPrune:
		return "prune"
	case ActionUnlock:
		return "unlock"
	case ActionInit:
		return "initialize"
	case ActionScheduledCheck:
		return "scheduled check"
	case ActionManualCheck:
		return "check"
	case ActionTag:
		return "tag"
	default:
		return fmt.Sprintf("action(%d)", int(a))
	}
}

// ErrNotAllowed is returned (wrapped) when the repo mode forbids an action.
var ErrNotAllowed = errors.New("not allowed by repo mode")

// Mode returns the repo's mode. A nil repo is MANAGED.
func Mode(repo *v1.Repo) v1.RepoMode {
	return repo.GetMode()
}

// Allows reports whether the mode permits the action.
func Allows(mode v1.RepoMode, a Action) bool {
	switch mode {
	case v1.RepoMode_REPO_MODE_APPEND_ONLY:
		switch a {
		case ActionForget, ActionPrune, ActionUnlock, ActionTag:
			return false
		}
		return true
	case v1.RepoMode_REPO_MODE_READ_ONLY:
		return a == ActionManualCheck
	default:
		return true
	}
}

// Check returns an error wrapping ErrNotAllowed if the repo's mode forbids the action.
func Check(repo *v1.Repo, a Action) error {
	mode := Mode(repo)
	if Allows(mode, a) {
		return nil
	}
	return fmt.Errorf("%s on repo %q: %w %s", a, repo.GetId(), ErrNotAllowed, ModeName(mode))
}

// NoLock reports whether restic commands for the repo must run with --no-lock.
// This is the case for READ_ONLY repos, where Backrest must not write lock
// files. The manual check is the only exception and must take its lock.
func NoLock(repo *v1.Repo) bool {
	return Mode(repo) == v1.RepoMode_REPO_MODE_READ_ONLY
}

// ModeName returns a short user-facing name for the mode, e.g. "READ_ONLY".
func ModeName(mode v1.RepoMode) string {
	switch mode {
	case v1.RepoMode_REPO_MODE_MANAGED:
		return "MANAGED"
	case v1.RepoMode_REPO_MODE_APPEND_ONLY:
		return "APPEND_ONLY"
	case v1.RepoMode_REPO_MODE_READ_ONLY:
		return "READ_ONLY"
	default:
		return mode.String()
	}
}
