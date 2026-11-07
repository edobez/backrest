package config

import (
	"errors"
	"fmt"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/adoption"
	"github.com/garethgeorge/backrest/internal/protoutil"
	"github.com/hashicorp/go-multierror"
)

// validateAdoptedSources checks the repo's adopted source rules. Mode conflicts (APPEND_ONLY and
// READ_ONLY repos) are reported by validateRepoMode.
func validateAdoptedSources(repo *v1.Repo) error {
	rules := repo.GetAdoptedSources()
	if len(rules) == 0 {
		return nil
	}

	var err error

	// The repo-level forget policy already applies to every snapshot in the repo, external ones
	// included. Combining both would apply two retention policies to the same snapshots.
	if protoutil.ScheduleEnabled(repo.GetForgetPolicy().GetSchedule()) {
		err = multierror.Append(err, errors.New("adopted sources cannot be combined with an enabled repo forget policy, disable one of them"))
	}

	for i, rule := range rules {
		prefix := fmt.Sprintf("adopted source %d (%s)", i, adoption.Describe(rule))
		if rule.GetHostname() == "" {
			err = multierror.Append(err, fmt.Errorf("%s: hostname is required", prefix))
		}
		if rule.GetPath() == "" {
			err = multierror.Append(err, fmt.Errorf("%s: path is required", prefix))
		}
		if rule.GetRetention() == nil {
			err = multierror.Append(err, fmt.Errorf("%s: retention policy is required", prefix))
		} else if e := protoutil.ValidateRetentionPolicy(rule.GetRetention()); e != nil {
			err = multierror.Append(err, fmt.Errorf("%s: %w", prefix, e))
		} else if !adoption.RemovesSnapshots(rule.GetRetention()) {
			err = multierror.Append(err, fmt.Errorf("%s: retention policy keeps every snapshot, remove the rule instead", prefix))
		}
		if rule.GetSchedule() != nil {
			if e := protoutil.ValidateSchedule(rule.GetSchedule()); e != nil {
				err = multierror.Append(err, fmt.Errorf("%s: schedule: %w", prefix, e))
			}
		}
		for _, other := range rules[:i] {
			if adoption.SameSelector(rule, other) {
				err = multierror.Append(err, fmt.Errorf("%s: duplicate rule, each host and path can be adopted once", prefix))
				break
			}
		}
	}

	return err
}
