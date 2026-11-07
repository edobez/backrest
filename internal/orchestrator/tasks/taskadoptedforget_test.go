package tasks

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/oplog"
	"github.com/garethgeorge/backrest/internal/oplog/sqlitestore"
	"github.com/garethgeorge/backrest/internal/orchestrator/repo"
	"github.com/garethgeorge/backrest/internal/sources"
	"github.com/garethgeorge/backrest/pkg/restic"
)

const adoptedTestGUID = "0000000000000000000000000000000000000000000000000000000000000001"

var adoptedSnapCounter int

func fakeSnap(host string, tags []string, paths ...string) restic.Snapshot {
	adoptedSnapCounter++
	return restic.Snapshot{
		Id:       fmt.Sprintf("%064x", adoptedSnapCounter),
		Time:     time.Unix(int64(adoptedSnapCounter)*3600, 0).UTC().Format(time.RFC3339Nano),
		Hostname: host,
		Paths:    paths,
		Tags:     tags,
	}
}

func group(keep, remove []restic.Snapshot) restic.ForgetGroup {
	any := append(slices.Clone(keep), remove...)[0]
	return restic.ForgetGroup{Host: any.Hostname, Paths: any.Paths, Keep: keep, Remove: remove}
}

type adoptedTestEnv struct {
	t      *testing.T
	runner *testTaskRunner
	orch   *fakeRepoOrchestrator
	cfg    *v1.Config
	now    time.Time
}

func newAdoptedTestEnv(t *testing.T) *adoptedTestEnv {
	t.Helper()
	store, err := sqlitestore.NewMemorySqliteStore(t)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	log, err := oplog.NewOpLog(store)
	if err != nil {
		t.Fatalf("create oplog: %v", err)
	}
	cfg := &v1.Config{
		Instance: "central",
		Repos: []*v1.Repo{{
			Id:   "repo",
			Guid: adoptedTestGUID,
			AdoptedSources: []*v1.AdoptedSource{{
				Hostname:  "nas",
				Path:      "/data",
				Retention: &v1.RetentionPolicy{Policy: &v1.RetentionPolicy_PolicyKeepLastN{PolicyKeepLastN: 1}},
				Schedule:  &v1.Schedule{Schedule: &v1.Schedule_MaxFrequencyDays{MaxFrequencyDays: 1}, Clock: v1.Schedule_CLOCK_LAST_RUN_TIME},
			}},
		}},
	}
	runner := newTestTaskRunner(t, cfg, log)
	orch := &fakeRepoOrchestrator{}
	runner.orchestrator = orch
	return &adoptedTestEnv{t: t, runner: runner, orch: orch, cfg: cfg, now: time.Unix(1_700_000_000, 0)}
}

func (e *adoptedTestEnv) task() Task {
	r := e.cfg.Repos[0]
	return NewAdoptedForgetTask(r, r.AdoptedSources[0], false)
}

// run executes one scheduled run of the task like the orchestrator does: Next, create the op, Run,
// record the outcome.
func (e *adoptedTestEnv) run(task Task) (*v1.Operation, error) {
	e.t.Helper()
	st, err := task.Next(e.now, e.runner)
	if err != nil {
		e.t.Fatalf("next: %v", err)
	}
	if st == NeverScheduledTask {
		e.t.Fatalf("task not scheduled")
	}
	st.Task = task
	op := st.Op
	op.RepoId = task.RepoID()
	op.RepoGuid = task.Repo().GetGuid()
	op.PlanId = task.PlanID()
	op.UnixTimeStartMs = st.RunAt.UnixMilli()
	op.Status = v1.OperationStatus_STATUS_INPROGRESS
	if err := e.runner.CreateOperation(op); err != nil {
		e.t.Fatalf("create op: %v", err)
	}
	runErr := task.Run(context.Background(), st, e.runner)
	op.Status = v1.OperationStatus_STATUS_SUCCESS
	if runErr != nil {
		op.Status = v1.OperationStatus_STATUS_ERROR
		op.DisplayMessage = runErr.Error()
	}
	op.UnixTimeEndMs = st.RunAt.Add(time.Minute).UnixMilli()
	if err := e.runner.UpdateOperation(op); err != nil {
		e.t.Fatalf("update op: %v", err)
	}
	e.now = st.RunAt.Add(time.Minute)
	return op, runErr
}

func (e *adoptedTestEnv) newSourceHooks() [][]*v1.SnapshotSource {
	var result [][]*v1.SnapshotSource
	for _, c := range e.runner.hookCalls {
		if slices.Contains(c.Events, v1.Hook_CONDITION_ADOPTED_SOURCE_NEW) {
			result = append(result, c.Vars.Sources)
		}
	}
	e.runner.hookCalls = nil
	return result
}

func TestAdoptedForgetAppliesRetentionPerSource(t *testing.T) {
	e := newAdoptedTestEnv(t)

	data := []restic.Snapshot{fakeSnap("nas", nil, "/data"), fakeSnap("nas", nil, "/data")}
	dataEtc := []restic.Snapshot{fakeSnap("nas", nil, "/data", "/etc"), fakeSnap("nas", nil, "/data", "/etc")}
	planTags := []string{repo.TagForPlan("myplan")}
	plan := []restic.Snapshot{fakeSnap("nas", planTags, "/data", "/plan"), fakeSnap("nas", planTags, "/data", "/plan")}
	e.orch.forgetDryRunGroups = []restic.ForgetGroup{
		group(data[1:], data[:1]),
		group(dataEtc[1:], dataEtc[:1]),
		group(plan[1:], plan[:1]),
		// restic's --path is a superset match, but a group the selector does not match must be
		// ignored even if restic returned it.
		group(nil, []restic.Snapshot{fakeSnap("nas", nil, "/other")}),
	}

	op, err := e.run(e.task())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	want := []string{data[0].Id, dataEtc[0].Id}
	if !slices.Equal(e.orch.forgotSnapshotIDs, want) {
		t.Errorf("forgot %v, want %v", e.orch.forgotSnapshotIDs, want)
	}

	adopted := op.GetOperationForget().GetAdopted()
	if adopted.GetHostname() != "nas" || adopted.GetPath() != "/data" || !adopted.GetResolved() {
		t.Errorf("unexpected adopted record: %v", adopted)
	}
	wantSources := []string{
		sources.KeyForSnapshot(&v1.ResticSnapshot{Hostname: "nas", Paths: []string{"/data"}}).ID(),
		sources.KeyForSnapshot(&v1.ResticSnapshot{Hostname: "nas", Paths: []string{"/data", "/etc"}}).ID(),
	}
	if !slices.Equal(adopted.GetSourceIds(), wantSources) {
		t.Errorf("source IDs %v, want %v (plan source must be excluded)", adopted.GetSourceIds(), wantSources)
	}
	if n := len(op.GetOperationForget().GetForget()); n != 2 {
		t.Errorf("recorded %d forgotten snapshots, want 2", n)
	}
	if !slices.Contains(e.orch.calls, "ForgetSnapshots") || slices.Contains(e.orch.calls, "Forget") {
		t.Errorf("expected forget by ID only, calls: %v", e.orch.calls)
	}
}

func TestAdoptedForgetNotifiesNewSources(t *testing.T) {
	e := newAdoptedTestEnv(t)
	data := fakeSnap("nas", nil, "/data")
	e.orch.forgetDryRunGroups = []restic.ForgetGroup{group([]restic.Snapshot{data}, nil)}

	// First run: everything is new.
	if _, err := e.run(e.task()); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if hooks := e.newSourceHooks(); len(hooks) != 1 || len(hooks[0]) != 1 {
		t.Fatalf("run 1: expected one notification for one source, got %v", hooks)
	}

	// Second run, same sources: no notification.
	if _, err := e.run(e.task()); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if hooks := e.newSourceHooks(); len(hooks) != 0 {
		t.Fatalf("run 2: expected no notification, got %v", hooks)
	}

	// The external tool adds a path: a new source matches.
	dataEtc := fakeSnap("nas", nil, "/data", "/etc")
	e.orch.forgetDryRunGroups = append(e.orch.forgetDryRunGroups, group([]restic.Snapshot{dataEtc}, nil))
	if _, err := e.run(e.task()); err != nil {
		t.Fatalf("run 3: %v", err)
	}
	hooks := e.newSourceHooks()
	if len(hooks) != 1 || len(hooks[0]) != 1 || !slices.Equal(hooks[0][0].GetPaths(), []string{"/data", "/etc"}) {
		t.Fatalf("run 3: expected a notification for [/data /etc] only, got %v", hooks)
	}
}

func TestAdoptedForgetCancelledByNewSourceHook(t *testing.T) {
	e := newAdoptedTestEnv(t)
	snaps := []restic.Snapshot{fakeSnap("nas", nil, "/data"), fakeSnap("nas", nil, "/data")}
	e.orch.forgetDryRunGroups = []restic.ForgetGroup{group(snaps[1:], snaps[:1])}

	e.runner.onExecuteHooks = func(ctx context.Context, events []v1.Hook_Condition, vars HookVars) error {
		if slices.Contains(events, v1.Hook_CONDITION_ADOPTED_SOURCE_NEW) {
			return fmt.Errorf("cancelled by hook")
		}
		return nil
	}
	if _, err := e.run(e.task()); err == nil {
		t.Fatal("expected the hook error to fail the run")
	}
	if len(e.orch.forgotSnapshotIDs) != 0 {
		t.Fatalf("forgot %v although the hook cancelled the run", e.orch.forgotSnapshotIDs)
	}
	if hooks := e.newSourceHooks(); len(hooks) != 1 {
		t.Fatalf("expected one notification, got %d", len(hooks))
	}

	// The cancelled run did not record the sources, so they are still reported as new.
	e.runner.onExecuteHooks = nil
	if _, err := e.run(e.task()); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if hooks := e.newSourceHooks(); len(hooks) != 1 {
		t.Fatalf("run 2: expected the source to be reported again, got %d notifications", len(hooks))
	}
	if !slices.Equal(e.orch.forgotSnapshotIDs, []string{snaps[0].Id}) {
		t.Errorf("run 2 forgot %v, want %v", e.orch.forgotSnapshotIDs, []string{snaps[0].Id})
	}
}

func TestAdoptedForgetRefusesMixedSource(t *testing.T) {
	e := newAdoptedTestEnv(t)
	mixed := []restic.Snapshot{fakeSnap("nas", nil, "/data"), fakeSnap("nas", []string{repo.TagForPlan("myplan")}, "/data")}
	other := []restic.Snapshot{fakeSnap("nas", nil, "/data", "/etc"), fakeSnap("nas", nil, "/data", "/etc")}
	e.orch.forgetDryRunGroups = []restic.ForgetGroup{
		group(mixed[1:], mixed[:1]),
		group(other[1:], other[:1]),
	}

	op, err := e.run(e.task())
	if err == nil || !strings.Contains(err.Error(), "refused 1 source") || !strings.Contains(err.Error(), "plan myplan") {
		t.Fatalf("expected an explicit refusal error, got %v", err)
	}
	if !slices.Equal(e.orch.forgotSnapshotIDs, []string{other[0].Id}) {
		t.Errorf("forgot %v, want only %v from the unmixed source", e.orch.forgotSnapshotIDs, other[0].Id)
	}
	if n := len(op.GetOperationForget().GetAdopted().GetRefusedSourceIds()); n != 1 {
		t.Errorf("recorded %d refused sources, want 1", n)
	}
}

func TestAdoptedForgetStopsWhenRuleRemoved(t *testing.T) {
	e := newAdoptedTestEnv(t)
	task := e.task()
	st, err := task.Next(e.now, e.runner)
	if err != nil || st == NeverScheduledTask {
		t.Fatalf("expected the task to be scheduled, got %v, %v", st, err)
	}

	e.cfg.Repos[0].AdoptedSources = nil

	st2, err := task.Next(e.now, e.runner)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if st2 != NeverScheduledTask {
		t.Errorf("task still scheduled after the rule was removed")
	}

	// A run that was already queued must not touch anything either.
	st.Task = task
	if err := task.Run(context.Background(), st, e.runner); err == nil {
		t.Errorf("expected an error for a removed rule")
	}
	if len(e.orch.calls) != 0 {
		t.Errorf("repo was used after the rule was removed: %v", e.orch.calls)
	}
}

func TestAdoptedForgetNotScheduledOnAppendOnlyRepo(t *testing.T) {
	e := newAdoptedTestEnv(t)
	e.cfg.Repos[0].Mode = v1.RepoMode_REPO_MODE_APPEND_ONLY
	st, err := e.task().Next(e.now, e.runner)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if st != NeverScheduledTask {
		t.Errorf("adopted forget scheduled on an APPEND_ONLY repo")
	}
}

func TestAdoptedForgetSchedulesFromLastRun(t *testing.T) {
	e := newAdoptedTestEnv(t)
	e.orch.forgetDryRunGroups = []restic.ForgetGroup{group([]restic.Snapshot{fakeSnap("nas", nil, "/data")}, nil)}

	// Without history the first run waits a full interval.
	st, err := e.task().Next(e.now, e.runner)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if got, want := st.RunAt, e.now.Add(24*time.Hour); !got.Equal(want) {
		t.Errorf("first run at %v, want %v", got, want)
	}

	if _, err := e.run(e.task()); err != nil {
		t.Fatalf("run: %v", err)
	}
	lastEnd := e.now
	st, err = e.task().Next(e.now, e.runner)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if got, want := st.RunAt, lastEnd.Add(24*time.Hour); !got.Equal(want) {
		t.Errorf("next run at %v, want %v", got, want)
	}
}
