// Package sources groups snapshots into sources: the (hostname, paths) tuple restic uses for
// `--group-by host,paths`. Tags are deliberately not part of the key, so Backrest's own `plan:` and
// `created-by:` tags don't split a source; they are reported as metadata instead.
package sources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sort"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/orchestrator/repo"
)

// sourceIDLen is the number of hex characters kept from the key hash.
const sourceIDLen = 16

// Key identifies a source. Paths are sorted with a plain byte-order sort, exactly as restic's
// data.GroupSnapshots does. No other normalization is applied: restic already stores paths as
// absolute, cleaned paths at backup time, and diverging from restic here would make Backrest's
// groups disagree with what `restic forget --group-by host,paths` acts on.
type Key struct {
	Hostname string   `json:"hostname"`
	Paths    []string `json:"paths"`
}

// KeyForSnapshot returns the source key of a snapshot. The snapshot is not modified.
func KeyForSnapshot(snap *v1.ResticSnapshot) Key {
	paths := slices.Clone(snap.GetPaths())
	if paths == nil {
		paths = []string{}
	}
	sort.Strings(paths)
	return Key{Hostname: snap.GetHostname(), Paths: paths}
}

// ID returns a stable, URL-safe identifier for the key. It does not include the repo, so the
// same host and paths produce the same ID in every repo.
func (k Key) ID() string {
	// json.Marshal of a struct with string fields cannot fail.
	b, _ := json.Marshal(k)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:sourceIDLen]
}

// IDForSnapshot is a shorthand for KeyForSnapshot(snap).ID().
func IDForSnapshot(snap *v1.ResticSnapshot) string {
	return KeyForSnapshot(snap).ID()
}

// Build groups the given snapshots into sources for the given repo. Snapshots with a duplicate
// ID are counted once. The result is sorted by hostname, then by paths.
func Build(repoID string, snapshots []*v1.ResticSnapshot) []*v1.SnapshotSource {
	type acc struct {
		source    *v1.SnapshotSource
		plans     map[string]struct{}
		instances map[string]struct{}
	}

	byID := make(map[string]*acc)
	seen := make(map[string]struct{}, len(snapshots))

	for _, snap := range snapshots {
		if _, ok := seen[snap.GetId()]; ok {
			continue
		}
		seen[snap.GetId()] = struct{}{}

		key := KeyForSnapshot(snap)
		id := key.ID()
		a, ok := byID[id]
		if !ok {
			a = &acc{
				source: &v1.SnapshotSource{
					Id:                  id,
					RepoId:              repoID,
					Hostname:            key.Hostname,
					Paths:               key.Paths,
					FirstSnapshotTimeMs: snap.GetUnixTimeMs(),
				},
				plans:     make(map[string]struct{}),
				instances: make(map[string]struct{}),
			}
			byID[id] = a
		}

		s := a.source
		s.SnapshotCount++
		if t := snap.GetUnixTimeMs(); t < s.FirstSnapshotTimeMs {
			s.FirstSnapshotTimeMs = t
		}
		if t := snap.GetUnixTimeMs(); s.LastSnapshotId == "" || t > s.LastSnapshotTimeMs {
			s.LastSnapshotTimeMs = t
			s.LastSnapshotId = snap.GetId()
			s.LastSnapshotBytes = snap.GetSummary().GetTotalBytesProcessed()
		}
		if p := repo.PlanFromTags(snap.GetTags()); p != "" {
			a.plans[p] = struct{}{}
		}
		if i := repo.InstanceIDFromTags(snap.GetTags()); i != "" {
			a.instances[i] = struct{}{}
		}
	}

	result := make([]*v1.SnapshotSource, 0, len(byID))
	for _, a := range byID {
		a.source.PlanIds = sortedKeys(a.plans)
		a.source.InstanceIds = sortedKeys(a.instances)
		result = append(result, a.source)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Hostname != result[j].Hostname {
			return result[i].Hostname < result[j].Hostname
		}
		return slices.Compare(result[i].Paths, result[j].Paths) < 0
	})
	return result
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
