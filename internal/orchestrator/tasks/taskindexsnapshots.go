package tasks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/oplog"
	"github.com/garethgeorge/backrest/internal/orchestrator/repo"
	"github.com/garethgeorge/backrest/internal/protoutil"
	"go.uber.org/zap"
)

func NewOneoffIndexSnapshotsTask(repo *v1.Repo, at time.Time) Task {
	return &GenericOneoffTask{
		BaseTask: BaseTask{
			TaskType: "index_snapshots",
			TaskName: fmt.Sprintf("index snapshots for repo %q", repo.Id),
			TaskRepo: repo,
		},
		RunAt:   at,
		ProtoOp: nil,
		Do: func(ctx context.Context, st ScheduledTask, taskRunner TaskRunner) error {
			return NotifyError(ctx, taskRunner, st.Task.Name(), indexSnapshotsHelper(ctx, st, taskRunner))
		},
	}
}

// lastIndexRun records when each repo (by GUID) was last indexed successfully. Index runs don't create
// an operation in the oplog, so this is kept in memory. It survives config changes (which recreate
// all scheduled tasks) but not restarts, so a scheduled index runs once shortly after startup.
var lastIndexRun = struct {
	sync.Mutex
	byGUID map[string]time.Time
}{byGUID: make(map[string]time.Time)}

func recordIndexRun(repoGUID string, at time.Time) {
	lastIndexRun.Lock()
	defer lastIndexRun.Unlock()
	lastIndexRun.byGUID[repoGUID] = at
}

func lastIndexRunFor(repoGUID string) time.Time {
	lastIndexRun.Lock()
	defer lastIndexRun.Unlock()
	return lastIndexRun.byGUID[repoGUID]
}

// ScheduledIndexSnapshotsTask re-indexes a repo's snapshots according to the repo's index policy, so
// snapshots created by other clients are picked up even when Backrest runs no backups on the repo.
type ScheduledIndexSnapshotsTask struct {
	BaseTask
}

func NewScheduledIndexSnapshotsTask(repo *v1.Repo) Task {
	return &ScheduledIndexSnapshotsTask{
		BaseTask: BaseTask{
			TaskType:   "scheduled_index_snapshots",
			TaskName:   fmt.Sprintf("scheduled index snapshots for repo %q", repo.Id),
			TaskRepo:   repo,
			TaskPlanID: PlanForSystemTasks,
		},
	}
}

func (t *ScheduledIndexSnapshotsTask) Next(now time.Time, runner TaskRunner) (ScheduledTask, error) {
	repoProto, err := runner.GetRepo(t.RepoID())
	if err != nil {
		return ScheduledTask{}, fmt.Errorf("get repo %v: %w", t.RepoID(), err)
	}

	schedule := repoProto.GetIndexPolicy().GetSchedule()
	if !protoutil.ScheduleEnabled(schedule) {
		return NeverScheduledTask, nil
	}

	runAt, err := protoutil.ResolveSchedule(schedule, lastIndexRunFor(repoProto.GetGuid()), now)
	if errors.Is(err, protoutil.ErrScheduleDisabled) {
		return NeverScheduledTask, nil
	} else if err != nil {
		return NeverScheduledTask, fmt.Errorf("resolve schedule: %w", err)
	}
	if runAt.Before(now) {
		runAt = now
	}

	return ScheduledTask{RunAt: runAt}, nil
}

func (t *ScheduledIndexSnapshotsTask) Run(ctx context.Context, st ScheduledTask, runner TaskRunner) error {
	return NotifyError(ctx, runner, t.Name(), indexSnapshotsHelper(ctx, st, runner))
}

// indexSnapshotsHelper indexes all snapshots for a plan.
//   - If the snapshot is already indexed, it is skipped.
//   - If the snapshot is not indexed, an index snapshot operation with it's metadata is added.
//   - If an index snapshot operation is found for a snapshot that is not returned by the repo, it is marked as forgotten.
func indexSnapshotsHelper(ctx context.Context, st ScheduledTask, taskRunner TaskRunner) error {
	t := st.Task
	l := taskRunner.Logger(ctx)

	repo, err := taskRunner.GetRepoOrchestrator(t.RepoID())
	if err != nil {
		return fmt.Errorf("couldn't get repo %q: %w", t.RepoID(), err)
	}

	// collect all tracked snapshots for the plan.
	snapshots, err := repo.Snapshots(ctx)
	if err != nil {
		return fmt.Errorf("get snapshots for repo %q: %w", t.RepoID(), err)
	}

	// collect all current snapshot IDs.
	currentIds, err := indexCurrentSnapshotIdsForRepo(taskRunner, t.Repo().GetGuid())
	if err != nil {
		return fmt.Errorf("get known snapshot IDs for repo %q: %w", t.RepoID(), err)
	}

	foundIds := make(map[string]struct{})

	// Index newly found operations
	var indexOps []*v1.Operation
	for _, snapshot := range snapshots {
		if _, ok := currentIds[snapshot.Id]; ok {
			foundIds[snapshot.Id] = struct{}{}
			continue
		}

		snapshotProto := protoutil.SnapshotToProto(snapshot)
		flowID, err := FlowIDForSnapshotID(taskRunner, t.Repo().GetGuid(), snapshot.Id)
		if err != nil {
			return fmt.Errorf("get flow ID for snapshot %q: %w", snapshot.Id, err)
		}
		planId := planForSnapshot(snapshotProto)
		instanceID := instanceIDForSnapshot(snapshotProto)
		indexOps = append(indexOps, &v1.Operation{
			RepoId:          t.Repo().Id,
			RepoGuid:        t.Repo().Guid,
			PlanId:          planId,
			FlowId:          flowID,
			InstanceId:      instanceID,
			UnixTimeStartMs: snapshotProto.UnixTimeMs,
			UnixTimeEndMs:   snapshotProto.UnixTimeMs + snapshot.SnapshotSummary.DurationMs(),
			Status:          v1.OperationStatus_STATUS_SUCCESS,
			SnapshotId:      snapshotProto.Id,
			Op: &v1.Operation_OperationIndexSnapshot{
				OperationIndexSnapshot: &v1.OperationIndexSnapshot{
					Snapshot: snapshotProto,
				},
			},
		})
	}

	l.Sugar().Debugf("adding %v new snapshots to the oplog", len(indexOps))
	l.Sugar().Debugf("found %v snapshots already indexed", len(foundIds))

	if err := taskRunner.CreateOperation(indexOps...); err != nil {
		return fmt.Errorf("Create snapshot operations: %w", err)
	}

	// Mark missing operations as newly forgotten.
	for id, opId := range currentIds {
		if _, ok := foundIds[id]; ok {
			// skip snapshots that were found.
			continue
		}

		// mark snapshot forgotten.
		op, err := taskRunner.GetOperation(opId)
		if err != nil {
			// should only be possible in the case of a data race (e.g. operation was somehow deleted).
			return fmt.Errorf("get operation %v: %w", opId, err)
		}

		snapshotOp, ok := op.Op.(*v1.Operation_OperationIndexSnapshot)
		if !ok {
			return fmt.Errorf("operation %v is not an index snapshot operation", opId)
		}
		snapshotOp.OperationIndexSnapshot.Forgot = true

		if err := taskRunner.UpdateOperation(op); err != nil {
			return fmt.Errorf("mark index snapshot operation %v as forgotten: %w", opId, err)
		}
	}

	l.Sugar().Debugf("marked %v snapshots as forgotten", len(currentIds)-len(foundIds))

	recordIndexRun(t.Repo().GetGuid(), time.Now())
	return nil
}

// returns a map of current (e.g. not forgotten) snapshot IDs for the plan.
func indexCurrentSnapshotIdsForRepo(taskRunner TaskRunner, repoGUID string) (map[string]int64, error) {
	knownIds := make(map[string]int64)

	startTime := time.Now()
	if err := taskRunner.QueryOperations(oplog.Query{}.
		SetRepoGUID(repoGUID),
		func(op *v1.Operation) error {
			if snapshotOp, ok := op.Op.(*v1.Operation_OperationIndexSnapshot); ok {
				if !snapshotOp.OperationIndexSnapshot.Forgot {
					knownIds[snapshotOp.OperationIndexSnapshot.Snapshot.Id] = op.Id
				}
			}
			return nil
		}); err != nil {
		return nil, err
	}
	zap.S().Debugf("found %v known snapshot IDs for repo %v in %v", len(knownIds), repoGUID, time.Since(startTime))
	return knownIds, nil
}

func planForSnapshot(snapshot *v1.ResticSnapshot) string {
	p := repo.PlanFromTags(snapshot.Tags)
	if p != "" {
		return p
	}
	return PlanForUnassociatedOperations
}

func instanceIDForSnapshot(snapshot *v1.ResticSnapshot) string {
	id := repo.InstanceIDFromTags(snapshot.Tags)
	if id != "" {
		return id
	}
	return InstanceIDForUnassociatedOperations
}
