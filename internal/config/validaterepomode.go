package config

import (
	"fmt"
	"strings"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/protoutil"
	"github.com/garethgeorge/backrest/internal/repomode"
)

// validateRepoMode checks that nothing in the config asks Backrest to do what
// the repo's mode forbids. All conflicts for the repo are reported in a single
// error so the user knows everything that must be removed.
func validateRepoMode(repo *v1.Repo, plans []*v1.Plan) error {
	mode := repo.GetMode()
	if _, ok := v1.RepoMode_name[int32(mode)]; !ok {
		return fmt.Errorf("invalid mode value %d", mode)
	}
	if mode == v1.RepoMode_REPO_MODE_MANAGED {
		return nil
	}

	var conflicts []string

	for _, plan := range plans {
		if plan.GetRepo() != repo.GetId() {
			continue
		}
		if !repomode.Allows(mode, repomode.ActionBackup) {
			conflicts = append(conflicts, fmt.Sprintf("plan %q (backups are not allowed)", plan.GetId()))
		} else if planForgets(plan) {
			conflicts = append(conflicts, fmt.Sprintf("retention policy of plan %q", plan.GetId()))
		}
	}

	if !repomode.Allows(mode, repomode.ActionForget) && protoutil.ScheduleEnabled(repo.GetForgetPolicy().GetSchedule()) {
		conflicts = append(conflicts, "forget policy schedule")
	}
	if !repomode.Allows(mode, repomode.ActionForget) && len(repo.GetAdoptedSources()) > 0 {
		conflicts = append(conflicts, "adopted sources")
	}
	if !repomode.Allows(mode, repomode.ActionPrune) && protoutil.ScheduleEnabled(repo.GetPrunePolicy().GetSchedule()) {
		conflicts = append(conflicts, "prune policy schedule")
	}
	if !repomode.Allows(mode, repomode.ActionScheduledCheck) && protoutil.ScheduleEnabled(repo.GetCheckPolicy().GetSchedule()) {
		conflicts = append(conflicts, "check policy schedule")
	}
	if !repomode.Allows(mode, repomode.ActionUnlock) && repo.GetAutoUnlock() {
		conflicts = append(conflicts, "auto unlock")
	}
	if !repomode.Allows(mode, repomode.ActionInit) && repo.GetAutoInitialize() {
		conflicts = append(conflicts, "auto initialize")
	}

	if len(conflicts) == 0 {
		return nil
	}
	return fmt.Errorf("mode %s does not allow the following, remove or disable them first: %s",
		repomode.ModeName(mode), strings.Join(conflicts, ", "))
}

// planForgets reports whether backups of the plan are followed by a forget,
// mirroring the condition used by the backup task.
func planForgets(plan *v1.Plan) bool {
	if plan.GetRetention() == nil {
		return false
	}
	_, keepAll := plan.GetRetention().GetPolicy().(*v1.RetentionPolicy_PolicyKeepAll)
	return !keepAll
}
