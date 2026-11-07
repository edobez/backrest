// Package adoption applies adopted source rules: retention for external snapshot sources, i.e.
// snapshots without a plan: tag, typically created by other tools.
//
// restic cannot express "exactly this source" or "without tag plan:*" as a filter: --path is a
// superset match and --tag has no negation. Retention therefore runs in two steps. A dry run with
// `--group-by host,paths --host <h> --path <p>` lets restic compute keep/remove per group. Select
// then keeps only the groups whose (host, paths) is a source the rule matches, skips sources made of
// plan snapshots only (their plan manages them), refuses sources that mix plan and external
// snapshots, and returns the IDs to forget. Only those IDs are forgotten afterwards,
// so snapshots added between the two steps are never removed.
package adoption

import (
	"fmt"
	"slices"
	"strings"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/orchestrator/repo"
	"github.com/garethgeorge/backrest/internal/protoutil"
	"github.com/garethgeorge/backrest/internal/sources"
	"github.com/garethgeorge/backrest/pkg/restic"
)

// Match is one source matched by a rule, with restic's keep/remove decision for it.
type Match struct {
	Source *v1.SnapshotSource
	Keep   []*v1.ResticSnapshot
	Remove []*v1.ResticSnapshot
	// RefusedReason is set when the rule does not manage the source. Remove is then empty.
	RefusedReason string
	// Skipped is set, together with RefusedReason, when the source contains only plan snapshots.
	// Such a source is managed by its plan; the rule ignores it and that is not an error.
	Skipped bool
}

// Refused reports whether the rule refuses to manage the source because it mixes plan snapshots
// and external snapshots. This is an error: the plan snapshots would distort the retention counts.
func (m Match) Refused() bool {
	return m.RefusedReason != "" && !m.Skipped
}

// Adopted reports whether the rule manages the source.
func (m Match) Adopted() bool {
	return m.RefusedReason == ""
}

// MatchesKey reports whether the rule's selector matches the source key: exact hostname, and the
// rule's path is one of the source's paths.
func MatchesKey(rule *v1.AdoptedSource, key sources.Key) bool {
	return key.Hostname == rule.GetHostname() && slices.Contains(key.Paths, rule.GetPath())
}

// SameSelector reports whether two rules have the same selector. The selector identifies a rule.
func SameSelector(a, b *v1.AdoptedSource) bool {
	return a.GetHostname() == b.GetHostname() && a.GetPath() == b.GetPath()
}

// Find returns the repo's rule with the given selector, or nil.
func Find(repoCfg *v1.Repo, hostname, path string) *v1.AdoptedSource {
	for _, r := range repoCfg.GetAdoptedSources() {
		if r.GetHostname() == hostname && r.GetPath() == path {
			return r
		}
	}
	return nil
}

// Describe returns a short user-facing description of the rule's selector.
func Describe(rule *v1.AdoptedSource) string {
	return fmt.Sprintf("host %q, path %q", rule.GetHostname(), rule.GetPath())
}

// Select picks the groups of a `forget --dry-run --group-by host,paths` result that the rule
// manages. Groups whose source the rule does not match are ignored. A matched source made only of
// snapshots with a plan: tag is skipped; one that mixes them with external snapshots is refused.
// The result is sorted like sources.Build.
func Select(repoID string, rule *v1.AdoptedSource, groups []restic.ForgetGroup) ([]Match, error) {
	var matches []Match
	seen := make(map[string]struct{})

	for _, g := range groups {
		keep := snapshotsToProto(g.Keep)
		remove := snapshotsToProto(g.Remove)
		all := append(slices.Clone(keep), remove...)
		if len(all) == 0 {
			continue
		}

		// Derive the key from the snapshots rather than trusting the group header, and make sure
		// restic grouped exactly one source here.
		key := sources.KeyForSnapshot(all[0])
		for _, s := range all[1:] {
			k := sources.KeyForSnapshot(s)
			if k.Hostname != key.Hostname || !slices.Equal(k.Paths, key.Paths) {
				return nil, fmt.Errorf("forget group mixes sources (%q %v and %q %v), expected grouping by host and paths",
					key.Hostname, key.Paths, k.Hostname, k.Paths)
			}
		}
		if !MatchesKey(rule, key) {
			continue
		}
		if _, ok := seen[key.ID()]; ok {
			return nil, fmt.Errorf("forget result contains source %q %v twice", key.Hostname, key.Paths)
		}
		seen[key.ID()] = struct{}{}

		built := sources.Build(repoID, all)
		if len(built) != 1 {
			return nil, fmt.Errorf("expected one source for group %q %v, got %d", key.Hostname, key.Paths, len(built))
		}
		m := Match{Source: built[0], Keep: keep, Remove: remove}
		if plans := built[0].GetPlanIds(); len(plans) > 0 {
			planSnapshots := 0
			for _, s := range all {
				if repo.PlanFromTags(s.GetTags()) != "" {
					planSnapshots++
				}
			}
			if planSnapshots == len(all) {
				m.Skipped = true
				m.RefusedReason = fmt.Sprintf("managed by plan %s", strings.Join(plans, ", "))
			} else {
				m.RefusedReason = fmt.Sprintf("source mixes %d snapshot(s) of plan %s with %d external snapshot(s); only sources without plan snapshots can be adopted",
					planSnapshots, strings.Join(plans, ", "), len(all)-planSnapshots)
			}
			m.Remove = nil
		}
		matches = append(matches, m)
	}

	slices.SortFunc(matches, func(a, b Match) int {
		if c := strings.Compare(a.Source.GetHostname(), b.Source.GetHostname()); c != 0 {
			return c
		}
		return slices.Compare(a.Source.GetPaths(), b.Source.GetPaths())
	})
	return matches, nil
}

// RemoveIDs returns the IDs of the snapshots to forget: the remove lists of all adopted matches.
func RemoveIDs(matches []Match) []string {
	var ids []string
	for _, m := range matches {
		if !m.Adopted() {
			continue
		}
		for _, s := range m.Remove {
			ids = append(ids, s.GetId())
		}
	}
	return ids
}

// ToProto converts matches to their API representation.
func ToProto(matches []Match) []*v1.AdoptedSourceMatch {
	result := make([]*v1.AdoptedSourceMatch, 0, len(matches))
	for _, m := range matches {
		result = append(result, &v1.AdoptedSourceMatch{
			Source:        m.Source,
			Keep:          m.Keep,
			Remove:        m.Remove,
			RefusedReason: m.RefusedReason,
			Skipped:       m.Skipped,
		})
	}
	return result
}

// RemovesSnapshots reports whether the retention policy can remove anything, i.e. it is not
// keep-all and sets at least one keep count.
func RemovesSnapshots(policy *v1.RetentionPolicy) bool {
	p := protoutil.RetentionPolicyFromProto(policy)
	return p != nil && *p != (restic.RetentionPolicy{})
}

func snapshotsToProto(snaps []restic.Snapshot) []*v1.ResticSnapshot {
	result := make([]*v1.ResticSnapshot, 0, len(snaps))
	for i := range snaps {
		result = append(result, protoutil.SnapshotToProto(&snaps[i]))
	}
	return result
}
