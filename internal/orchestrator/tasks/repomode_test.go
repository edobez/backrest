package tasks

import (
	"context"
	"testing"
	"time"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/repomode"
	"github.com/garethgeorge/backrest/pkg/restic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var restrictedModes = []v1.RepoMode{
	v1.RepoMode_REPO_MODE_APPEND_ONLY,
	v1.RepoMode_REPO_MODE_READ_ONLY,
}

func dailySchedule() *v1.Schedule {
	return &v1.Schedule{Schedule: &v1.Schedule_MaxFrequencyDays{MaxFrequencyDays: 1}}
}

// restrictedRepo returns a repo whose config asks for everything a restricted
// mode forbids. Validation rejects such a config; the task layer must refuse
// these operations regardless.
func restrictedRepo(mode v1.RepoMode) *v1.Repo {
	return &v1.Repo{
		Id:         "repo1",
		Guid:       "guid1",
		Mode:       mode,
		AutoUnlock: true,
		PrunePolicy: &v1.PrunePolicy{
			Schedule: dailySchedule(),
		},
		CheckPolicy: &v1.CheckPolicy{
			Schedule: dailySchedule(),
		},
		ForgetPolicy: &v1.ForgetPolicy{
			Schedule:  dailySchedule(),
			Retention: &v1.RetentionPolicy{Policy: &v1.RetentionPolicy_PolicyKeepLastN{PolicyKeepLastN: 5}},
		},
	}
}

func restrictedPlan() *v1.Plan {
	return &v1.Plan{
		Id:       "plan1",
		Repo:     "repo1",
		Paths:    []string{"/data"},
		Schedule: dailySchedule(),
		Retention: &v1.RetentionPolicy{
			Policy: &v1.RetentionPolicy_PolicyKeepLastN{PolicyKeepLastN: 5},
		},
	}
}

func TestRepoModeBlocksScheduledTasks(t *testing.T) {
	for _, mode := range restrictedModes {
		t.Run(mode.String(), func(t *testing.T) {
			repo := restrictedRepo(mode)
			plan := restrictedPlan()
			runner := setupTestRunner(t, newTestConfig(repo, plan), &fakeRepoOrchestrator{})

			for _, task := range []Task{
				NewPruneTask(repo, PlanForSystemTasks, false),
				NewScheduledForgetTask(repo, PlanForSystemTasks, false),
			} {
				st, err := task.Next(time.Now(), runner)
				require.NoError(t, err)
				assert.Equal(t, NeverScheduledTask, st, "%s must not be scheduled", task.Type())
			}

			checkST, err := NewCheckTask(repo, PlanForSystemTasks, false).Next(time.Now(), runner)
			require.NoError(t, err)
			backupST, err := NewScheduledBackupTask(repo, plan).Next(time.Now(), runner)
			require.NoError(t, err)

			if mode == v1.RepoMode_REPO_MODE_READ_ONLY {
				assert.Equal(t, NeverScheduledTask, checkST, "scheduled check must not run on READ_ONLY")
				assert.Equal(t, NeverScheduledTask, backupST, "backup must not run on READ_ONLY")
			} else {
				assert.NotEqual(t, NeverScheduledTask, checkST, "scheduled check is allowed on APPEND_ONLY")
				assert.NotEqual(t, NeverScheduledTask, backupST, "backup is allowed on APPEND_ONLY")
			}
		})
	}
}

func TestRepoModeRefusesForcedTasks(t *testing.T) {
	type tc struct {
		name string
		task func(repo *v1.Repo, plan *v1.Plan) Task
		// allowedIn lists the modes in which the task may run.
		allowedIn []v1.RepoMode
	}
	tcs := []tc{
		{name: "manual prune", task: func(r *v1.Repo, _ *v1.Plan) Task { return NewPruneTask(r, PlanForSystemTasks, true) }},
		{name: "manual repo forget", task: func(r *v1.Repo, _ *v1.Plan) Task { return NewScheduledForgetTask(r, PlanForSystemTasks, true) }},
		{name: "plan forget", task: func(r *v1.Repo, p *v1.Plan) Task { return NewOneoffForgetTask(r, p.Id, 0, time.Now()) }},
		{name: "forget snapshot", task: func(r *v1.Repo, p *v1.Plan) Task {
			return NewOneoffForgetSnapshotTask(r, p.Id, 0, time.Now(), testSnapshotID)
		}},
		{
			name:      "manual backup",
			task:      func(r *v1.Repo, p *v1.Plan) Task { return NewOneoffBackupTask(r, p, time.Now(), false) },
			allowedIn: []v1.RepoMode{v1.RepoMode_REPO_MODE_APPEND_ONLY},
		},
	}

	for _, mode := range restrictedModes {
		for _, tc := range tcs {
			allowed := false
			for _, m := range tc.allowedIn {
				allowed = allowed || m == mode
			}
			if allowed {
				continue
			}
			t.Run(mode.String()+"/"+tc.name, func(t *testing.T) {
				repo := restrictedRepo(mode)
				plan := restrictedPlan()
				fake := &fakeRepoOrchestrator{}
				runner := setupTestRunner(t, newTestConfig(repo, plan), fake)

				task := tc.task(repo, plan)
				st := nextAndCreate(t, task, runner)
				err := task.Run(context.Background(), st, runner)
				require.ErrorIs(t, err, repomode.ErrNotAllowed)
				assert.Empty(t, fake.calls, "no repo operation may run, not even an unlock")
				assert.Empty(t, runner.scheduledTasks)
			})
		}
	}
}

func TestReadOnlyCheck(t *testing.T) {
	repo := restrictedRepo(v1.RepoMode_REPO_MODE_READ_ONLY)

	t.Run("scheduled check refuses to run", func(t *testing.T) {
		fake := &fakeRepoOrchestrator{}
		runner := setupTestRunner(t, newTestConfig(repo), fake)
		task := NewCheckTask(repo, PlanForSystemTasks, false)
		st := ScheduledTask{Task: task, RunAt: time.Now(), Op: &v1.Operation{RepoId: repo.Id, RepoGuid: repo.Guid, PlanId: PlanForSystemTasks, InstanceId: runner.InstanceID(), FlowId: 1, UnixTimeStartMs: time.Now().UnixMilli(), Op: &v1.Operation_OperationCheck{}}}
		require.NoError(t, runner.CreateOperation(st.Op))

		err := task.Run(context.Background(), st, runner)
		require.ErrorIs(t, err, repomode.ErrNotAllowed)
		assert.Empty(t, fake.calls)
	})

	t.Run("manual check runs", func(t *testing.T) {
		fake := &fakeRepoOrchestrator{}
		runner := setupTestRunner(t, newTestConfig(repo), fake)
		task := NewCheckTask(repo, PlanForSystemTasks, true)
		st := nextAndCreate(t, task, runner)

		require.NoError(t, task.Run(context.Background(), st, runner))
		assert.Contains(t, fake.calls, "Check")
	})
}

func TestAppendOnlyBackupSkipsForget(t *testing.T) {
	repo := &v1.Repo{Id: "repo1", Guid: "guid1", Mode: v1.RepoMode_REPO_MODE_APPEND_ONLY}
	plan := restrictedPlan()
	fake := &fakeRepoOrchestrator{
		backupResult: &restic.BackupProgressEntry{MessageType: "summary", SnapshotId: testSnapshotID},
	}
	runner := setupTestRunner(t, newTestConfig(repo, plan), fake)

	task := NewOneoffBackupTask(repo, plan, time.Now(), false)
	st := nextAndCreate(t, task, runner)
	require.NoError(t, task.Run(context.Background(), st, runner))

	var types []string
	for _, s := range runner.scheduledTasks {
		types = append(types, s.Task.Type())
	}
	assert.Equal(t, []string{"index_snapshots"}, types, "no forget may follow a backup on APPEND_ONLY")
}
