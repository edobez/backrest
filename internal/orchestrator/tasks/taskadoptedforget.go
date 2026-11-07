package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	v1 "github.com/garethgeorge/backrest/gen/go/v1"
	"github.com/garethgeorge/backrest/internal/adoption"
	"github.com/garethgeorge/backrest/internal/oplog"
	"github.com/garethgeorge/backrest/internal/protoutil"
	"github.com/garethgeorge/backrest/internal/repomode"
	"go.uber.org/zap"
)

// AdoptedForgetTask applies one adopted source rule of a repo: retention for the external sources
// the rule's selector matches. See package adoption for how sources are selected.
//
// The task identifies its rule by selector (hostname + path) and reads the rule from the current
// config on every Next and Run, so removing the rule stops all management of its sources.
type AdoptedForgetTask struct {
	BaseTask
	hostname string
	path     string
	force    bool
	didRun   bool
}

func NewAdoptedForgetTask(repoProto *v1.Repo, rule *v1.AdoptedSource, force bool) Task {
	return &AdoptedForgetTask{
		BaseTask: BaseTask{
			TaskType:   "adopted_forget",
			TaskName:   fmt.Sprintf("forget adopted sources (%s) in repo %q", adoption.Describe(rule), repoProto.Id),
			TaskRepo:   repoProto,
			TaskPlanID: PlanForSystemTasks,
		},
		hostname: rule.GetHostname(),
		path:     rule.GetPath(),
		force:    force,
	}
}

func (t *AdoptedForgetTask) newOp() *v1.Operation {
	return &v1.Operation{
		Op: &v1.Operation_OperationForget{
			OperationForget: &v1.OperationForget{
				Adopted: &v1.AdoptedForget{Hostname: t.hostname, Path: t.path},
			},
		},
	}
}

func (t *AdoptedForgetTask) Next(now time.Time, runner TaskRunner) (ScheduledTask, error) {
	if t.force {
		if t.didRun {
			return NeverScheduledTask, nil
		}
		t.didRun = true
		return ScheduledTask{RunAt: now, Op: t.newOp()}, nil
	}

	repoProto, err := runner.GetRepo(t.RepoID())
	if err != nil {
		return ScheduledTask{}, fmt.Errorf("get repo %v: %w", t.RepoID(), err)
	}
	rule := adoption.Find(repoProto, t.hostname, t.path)
	if rule == nil || !repomode.Allows(repoProto.GetMode(), repomode.ActionForget) {
		return NeverScheduledTask, nil
	}
	if !protoutil.ScheduleEnabled(rule.GetSchedule()) {
		return NeverScheduledTask, nil
	}

	hist, err := t.history(runner, repoProto.GetGuid(), 0)
	if err != nil {
		return NeverScheduledTask, fmt.Errorf("find last run of %s: %w", t.Name(), err)
	}
	lastRan := hist.lastRunEnd
	if lastRan.IsZero() {
		// Never ran: wait for one full interval after the rule was added, instead of deleting
		// snapshots the moment the config is saved.
		lastRan = now
	}

	runAt, err := protoutil.ResolveSchedule(rule.GetSchedule(), lastRan, now)
	if errors.Is(err, protoutil.ErrScheduleDisabled) {
		return NeverScheduledTask, nil
	} else if err != nil {
		return NeverScheduledTask, fmt.Errorf("resolve schedule: %w", err)
	}
	return ScheduledTask{RunAt: runAt, Op: t.newOp()}, nil
}

func (t *AdoptedForgetTask) Run(ctx context.Context, st ScheduledTask, runner TaskRunner) error {
	l := runner.Logger(ctx)
	notifyError := func(err error) error {
		return NotifyError(ctx, runner, t.Name(), err, v1.Hook_CONDITION_FORGET_ERROR)
	}

	repoProto, err := runner.GetRepo(t.RepoID())
	if err != nil {
		return notifyError(fmt.Errorf("get repo %q: %w", t.RepoID(), err))
	}
	rule := adoption.Find(repoProto, t.hostname, t.path)
	if rule == nil {
		return fmt.Errorf("adopted source rule (%s) no longer exists in repo %q", adoption.Describe(&v1.AdoptedSource{Hostname: t.hostname, Path: t.path}), repoProto.GetId())
	}
	if err := repomode.Check(repoProto, repomode.ActionForget); err != nil {
		return notifyError(err)
	}

	if st.Op.GetOperationForget() == nil {
		st.Op.Op = t.newOp().Op
	}
	forgetOp := st.Op.GetOperationForget()
	forgetOp.Policy = rule.GetRetention()
	forgetOp.Adopted = &v1.AdoptedForget{Hostname: t.hostname, Path: t.path}

	r, err := runner.GetRepoOrchestrator(t.RepoID())
	if err != nil {
		return notifyError(fmt.Errorf("get repo %q: %w", t.RepoID(), err))
	}
	if err := r.UnlockIfAutoEnabled(ctx); err != nil {
		return notifyError(fmt.Errorf("auto unlock repo %q: %w", t.RepoID(), err))
	}

	// Step 1: let restic compute keep/remove per source, without a lock and without removing anything.
	groups, err := r.ForgetDryRunBySource(ctx, rule.GetRetention(), t.hostname, t.path)
	if err != nil {
		return notifyError(err)
	}
	matches, err := adoption.Select(repoProto.GetId(), rule, groups)
	if err != nil {
		return notifyError(fmt.Errorf("select adopted sources: %w", err))
	}

	// Notify about sources the selector did not match on its previous run, before anything is
	// deleted from them. A failing hook (e.g. ON_ERROR_CANCEL) stops the run; the sources are then
	// still reported as new on the next run.
	hist, err := t.history(runner, repoProto.GetGuid(), st.Op.GetId())
	if err != nil {
		return notifyError(fmt.Errorf("find previous run of %s: %w", t.Name(), err))
	}
	if newSources := newlyMatched(matches, hist); len(newSources) > 0 {
		l.Sugar().Infof("adopted source rule (%s) matches %d new source(s)", adoption.Describe(rule), len(newSources))
		if err := runner.ExecuteHooks(ctx, []v1.Hook_Condition{
			v1.Hook_CONDITION_ADOPTED_SOURCE_NEW,
		}, HookVars{Sources: newSources}); err != nil {
			return notifyError(fmt.Errorf("adopted source new hook: %w", err))
		}
	}

	var refused []string
	for _, m := range matches {
		if m.Skipped {
			continue
		}
		forgetOp.Adopted.SourceIds = append(forgetOp.Adopted.SourceIds, m.Source.GetId())
		if m.Refused() {
			forgetOp.Adopted.RefusedSourceIds = append(forgetOp.Adopted.RefusedSourceIds, m.Source.GetId())
			refused = append(refused, fmt.Sprintf("%s %v: %s", m.Source.GetHostname(), m.Source.GetPaths(), m.RefusedReason))
		}
	}
	forgetOp.Adopted.Resolved = true

	if err := runner.ExecuteHooks(ctx, []v1.Hook_Condition{
		v1.Hook_CONDITION_FORGET_START,
	}, HookVars{}); err != nil {
		return notifyError(fmt.Errorf("forget start hook: %w", err))
	}

	// Step 2: forget exactly the snapshots computed above. Snapshots added in the meantime are kept.
	var removed []*v1.ResticSnapshot
	for _, m := range matches {
		if m.Adopted() {
			removed = append(removed, m.Remove...)
		}
	}
	if ids := adoption.RemoveIDs(matches); len(ids) > 0 {
		if err := r.ForgetSnapshots(ctx, ids); err != nil {
			return notifyError(err)
		}
	}
	forgetOp.Forget = removed
	l.Sugar().Infof("adopted source rule (%s): %d source(s) matched, %d refused, %d snapshot(s) forgotten",
		adoption.Describe(rule), len(matches), len(refused), len(removed))

	if err := markSnapshotsForgotten(ctx, runner, repoProto.GetGuid(), removed); err != nil {
		return notifyError(fmt.Errorf("forget: %w", err))
	}

	if len(refused) > 0 {
		return notifyError(fmt.Errorf("refused %d source(s), their snapshots were not touched: %s",
			len(refused), strings.Join(refused, "; ")))
	}

	if err := runner.ExecuteHooks(ctx, []v1.Hook_Condition{
		v1.Hook_CONDITION_FORGET_SUCCESS,
	}, HookVars{}); err != nil {
		return fmt.Errorf("forget end hook: %w", err)
	}

	if len(removed) > 0 {
		if e := runner.ScheduleTask(NewStatsTask(t.Repo(), PlanForSystemTasks, false), TaskPriorityStats); e != nil {
			zap.L().Error("schedule stats task", zap.Error(e))
		}
	}
	return nil
}

// adoptedHistory is what earlier runs of a rule recorded in the oplog.
type adoptedHistory struct {
	lastRunEnd time.Time // end of the most recent finished run, zero if none.
	// resolved is true if an earlier run resolved the selector; sourceIDs are then the sources it
	// matched.
	resolved  bool
	sourceIDs map[string]struct{}
}

// history scans the oplog for earlier runs of this rule, skipping the operation with ID skipOpID
// (the current run).
func (t *AdoptedForgetTask) history(runner TaskRunner, repoGUID string, skipOpID int64) (adoptedHistory, error) {
	var h adoptedHistory
	err := runner.QueryOperations(oplog.Query{}.
		SetRepoGUID(repoGUID).
		SetPlanID(PlanForSystemTasks).
		SetReversed(true), func(op *v1.Operation) error {
		if op.GetId() == skipOpID || op.GetUnixTimeEndMs() == 0 {
			return nil
		}
		switch op.GetStatus() {
		case v1.OperationStatus_STATUS_PENDING, v1.OperationStatus_STATUS_INPROGRESS:
			return nil
		}
		adopted := op.GetOperationForget().GetAdopted()
		if adopted == nil || adopted.GetHostname() != t.hostname || adopted.GetPath() != t.path {
			return nil
		}
		if h.lastRunEnd.IsZero() {
			h.lastRunEnd = time.UnixMilli(op.GetUnixTimeEndMs())
		}
		if adopted.GetResolved() {
			h.resolved = true
			h.sourceIDs = make(map[string]struct{}, len(adopted.GetSourceIds()))
			for _, id := range adopted.GetSourceIds() {
				h.sourceIDs[id] = struct{}{}
			}
			return oplog.ErrStopIteration
		}
		return nil
	})
	return h, err
}

// newlyMatched returns the sources of matches that the previous resolved run did not match. Without
// a previous resolved run, every matched source is new. Skipped (plan) sources are never reported.
func newlyMatched(matches []adoption.Match, h adoptedHistory) []*v1.SnapshotSource {
	var result []*v1.SnapshotSource
	for _, m := range matches {
		if m.Skipped {
			continue
		}
		if _, ok := h.sourceIDs[m.Source.GetId()]; h.resolved && ok {
			continue
		}
		result = append(result, m.Source)
	}
	return result
}
