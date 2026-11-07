package adoption_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/adoption"
	"github.com/garethgeorge/backrest/internal/orchestrator/repo"
	"github.com/garethgeorge/backrest/test/helpers"
)

// fixture is a real restic repo with snapshots created directly with the restic CLI, as an external
// tool would.
type fixture struct {
	t      *testing.T
	bin    string
	uri    string
	data   string
	etc    string
	plan   string
	other  string
	orch   *repo.RepoOrchestrator
	nextTS int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, bin: helpers.ResticBinary(t), uri: filepath.Join(root, "repo")}
	for _, d := range []*string{&f.data, &f.etc, &f.plan, &f.other} {
		dir, err := os.MkdirTemp(root, "src")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte(dir), 0o644); err != nil {
			t.Fatal(err)
		}
		*d = dir
	}
	f.restic("init")

	orch, err := repo.NewRepoOrchestrator(&v1.Config{Instance: "central"}, &v1.Repo{
		Id:       "test",
		Uri:      f.uri,
		Password: "test",
		Flags:    []string{"--no-cache"},
	}, f.bin)
	if err != nil {
		t.Fatalf("create orchestrator: %v", err)
	}
	f.orch = orch
	return f
}

func (f *fixture) restic(args ...string) string {
	f.t.Helper()
	cmd := exec.Command(f.bin, append(args, "--no-cache")...)
	cmd.Env = append(os.Environ(), "RESTIC_REPOSITORY="+f.uri, "RESTIC_PASSWORD=test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("restic %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// backup creates a snapshot of the given dirs for host, one day after the previous one.
func (f *fixture) backup(host string, tags []string, dirs ...string) {
	f.t.Helper()
	f.nextTS++
	args := []string{"backup", "--host", host, "--time", fmt.Sprintf("2024-01-%02d 12:00:00", f.nextTS)}
	for _, tag := range tags {
		args = append(args, "--tag", tag)
	}
	f.restic(append(args, dirs...)...)
}

// snapshotPath returns p as restic stores it in a snapshot. On Windows, C:\data is stored as /C/data.
func snapshotPath(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	if vol := filepath.VolumeName(p); vol != "" {
		return "/" + strings.TrimSuffix(vol, ":") + filepath.ToSlash(p[len(vol):])
	}
	return filepath.ToSlash(p)
}

// countBySource returns the number of snapshots per (host, sorted paths) key.
func (f *fixture) countBySource() map[string]int {
	f.t.Helper()
	snaps, err := f.orch.Snapshots(context.Background())
	if err != nil {
		f.t.Fatalf("snapshots: %v", err)
	}
	counts := map[string]int{}
	for _, s := range snaps {
		paths := slices.Clone(s.Paths)
		slices.Sort(paths)
		counts[s.Hostname+" "+strings.Join(paths, ",")]++
	}
	return counts
}

func (f *fixture) key(host string, dirs ...string) string {
	var paths []string
	for _, d := range dirs {
		paths = append(paths, snapshotPath(d))
	}
	slices.Sort(paths)
	return host + " " + strings.Join(paths, ",")
}

func keepLast(n int32) *v1.RetentionPolicy {
	return &v1.RetentionPolicy{Policy: &v1.RetentionPolicy_PolicyKeepLastN{PolicyKeepLastN: n}}
}

// apply runs the two-step adoption: dry run, select, forget exactly the selected IDs.
func (f *fixture) apply(rule *v1.AdoptedSource, beforeForget func()) []adoption.Match {
	f.t.Helper()
	ctx := context.Background()
	groups, err := f.orch.ForgetDryRunBySource(ctx, rule.GetRetention(), rule.GetHostname(), rule.GetPath())
	if err != nil {
		f.t.Fatalf("dry run: %v", err)
	}
	matches, err := adoption.Select("test", rule, groups)
	if err != nil {
		f.t.Fatalf("select: %v", err)
	}
	if beforeForget != nil {
		beforeForget()
	}
	if err := f.orch.ForgetSnapshots(ctx, adoption.RemoveIDs(matches)); err != nil {
		f.t.Fatalf("forget: %v", err)
	}
	return matches
}

func TestAdoptionIntegration(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("uses restic")
	}

	f := newFixture(t)
	planTags := []string{repo.TagForPlan("myplan"), repo.TagForInstance("central")}

	for i := 0; i < 4; i++ {
		f.backup("nas", nil, f.data) // external [/data]
	}
	for i := 0; i < 3; i++ {
		f.backup("nas", nil, f.data, f.etc) // external [/data, /etc]
	}
	for i := 0; i < 3; i++ {
		f.backup("nas", planTags, f.data, f.plan) // plan [/data, /plan]
	}
	f.backup("nas", nil, f.other)   // external, does not contain /data
	f.backup("laptop", nil, f.data) // external, other host
	f.backup("laptop", nil, f.data) // external, other host

	rule := &v1.AdoptedSource{Hostname: "nas", Path: snapshotPath(f.data), Retention: keepLast(2)}

	var addedMidway bool
	matches := f.apply(rule, func() {
		// A client backup between the dry run and the forget must never be removed.
		f.backup("nas", nil, f.data)
		addedMidway = true
	})
	if !addedMidway {
		t.Fatal("midway backup not run")
	}

	if len(matches) != 3 {
		t.Fatalf("expected 3 matched sources, got %d: %+v", len(matches), matches)
	}
	var adopted, skipped int
	for _, m := range matches {
		switch {
		case m.Refused():
			t.Errorf("unexpected refused source %v: %s", m.Source.GetPaths(), m.RefusedReason)
		case m.Skipped:
			skipped++
			if !slices.Equal(m.Source.GetPlanIds(), []string{"myplan"}) {
				t.Errorf("skipped source plan IDs = %v, want [myplan]", m.Source.GetPlanIds())
			}
		default:
			adopted++
		}
	}
	if adopted != 2 || skipped != 1 {
		t.Errorf("adopted=%d skipped=%d, want 2 and 1", adopted, skipped)
	}

	got := f.countBySource()
	want := map[string]int{
		f.key("nas", f.data):         3, // 4 -> keep 2, plus the one added between the two steps
		f.key("nas", f.data, f.etc):  2, // 3 -> keep 2, retention applied independently per source
		f.key("nas", f.data, f.plan): 3, // plan snapshots untouched
		f.key("nas", f.other):        1, // not matched
		f.key("laptop", f.data):      2, // other host, not matched
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("source %q: %d snapshots, want %d", k, got[k], n)
		}
	}
}

func TestAdoptionIntegrationRefusesMixedSource(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("uses restic")
	}

	f := newFixture(t)
	for i := 0; i < 3; i++ {
		f.backup("nas", nil, f.data) // external [/data]
	}
	f.backup("nas", []string{repo.TagForPlan("myplan")}, f.data) // plan snapshot in the same source
	for i := 0; i < 3; i++ {
		f.backup("nas", nil, f.data, f.etc) // external [/data, /etc]
	}

	rule := &v1.AdoptedSource{Hostname: "nas", Path: snapshotPath(f.data), Retention: keepLast(1)}
	matches := f.apply(rule, nil)

	var refused []adoption.Match
	for _, m := range matches {
		if m.Refused() {
			refused = append(refused, m)
		}
	}
	if len(refused) != 1 {
		t.Fatalf("expected 1 refused source, got %d: %+v", len(refused), matches)
	}
	if r := refused[0].RefusedReason; !strings.Contains(r, "plan myplan") || !strings.Contains(r, "mixes") {
		t.Errorf("refused reason %q does not explain the mix", r)
	}
	if len(refused[0].Remove) != 0 {
		t.Errorf("refused source has a remove list: %v", refused[0].Remove)
	}

	got := f.countBySource()
	if n := got[f.key("nas", f.data)]; n != 4 {
		t.Errorf("mixed source has %d snapshots, want all 4 kept", n)
	}
	if n := got[f.key("nas", f.data, f.etc)]; n != 1 {
		t.Errorf("other source has %d snapshots, want 1", n)
	}
}

func TestForgetDryRunNoMatch(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("uses restic")
	}

	f := newFixture(t)
	f.backup("nas", nil, f.data)

	groups, err := f.orch.ForgetDryRunBySource(context.Background(), keepLast(1), "nobody", snapshotPath(f.data))
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected no groups, got %+v", groups)
	}
}
