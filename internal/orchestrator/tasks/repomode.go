package tasks

import (
	"fmt"

	"github.com/garethgeorge/backrest/internal/repomode"
)

// checkRepoMode returns an error if the current config of the repo forbids the
// action. Tasks call it before doing any work, so that forbidden operations
// never run, whether they come from a schedule, a hook or the API.
func checkRepoMode(runner TaskRunner, repoID string, a repomode.Action) error {
	repo, err := runner.GetRepo(repoID)
	if err != nil {
		return fmt.Errorf("get repo %q: %w", repoID, err)
	}
	return repomode.Check(repo, a)
}

// repoModeAllows is the boolean form of checkRepoMode for scheduling decisions.
// A lookup error is treated as allowed so that the caller reports it instead.
func repoModeAllows(runner TaskRunner, repoID string, a repomode.Action) bool {
	repo, err := runner.GetRepo(repoID)
	if err != nil {
		return true
	}
	return repomode.Allows(repo.GetMode(), a)
}
