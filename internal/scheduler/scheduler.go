package scheduler

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
)

const Interval = 30 * time.Second

// Starter is the slice of *runner.Runner the scheduler needs.
type Starter interface {
	StartDump(ctx context.Context, databaseID, destID int64,
		label string, workflowID, createdBy int64) (jobID, dumpID int64, err error)
}

type Scheduler struct {
	st    *meta.Store
	start Starter
}

func New(st *meta.Store, start Starter) *Scheduler {
	return &Scheduler{st: st, start: start}
}

// Run blocks, ticking every Interval. Cancel ctx to stop.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.RunOnce(ctx, time.Now())
		}
	}
}

// RunOnce executes every due workflow. Time is injected so tests are
// deterministic. Semantics per spec:
//   - storage unavailable -> keep next_run_at (retried next tick)
//   - other failures      -> record last_error, still advance the schedule
//   - once                -> disables itself after a successful run
//   - cron                -> next run computed from now (missed runs never replay)
func (s *Scheduler) RunOnce(ctx context.Context, now time.Time) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("scheduler panic: %v", p)
		}
	}()
	due, err := s.st.DueWorkflows(ctx, now)
	if err != nil {
		log.Printf("scheduler: due workflows: %v", err)
		return
	}
	for i := range due {
		s.runWorkflow(ctx, due[i], now)
	}
}

func (s *Scheduler) runWorkflow(ctx context.Context, wf meta.Workflow, now time.Time) {
	lastErr := ""
	if _, _, err := s.start.StartDump(ctx, wf.DatabaseID, wf.DestID, wf.Name, wf.ID, 0); err != nil {
		lastErr = err.Error()
		if errors.Is(err, runner.ErrStorageUnavailable) {
			if err := s.st.SetWorkflowState(ctx, wf.ID, nil, lastErr, true, wf.NextRunAt); err != nil {
				log.Printf("scheduler: workflow %d state: %v", wf.ID, err)
			}
			return
		}
	}
	var next *time.Time
	enabled := true
	switch wf.TriggerKind {
	case "once":
		if lastErr == "" {
			enabled = false
		} else {
			next = wf.RunAt // retry next tick until it actually runs
		}
	case "cron":
		sched, err := cron.ParseStandard(wf.Cron)
		if err != nil {
			lastErr = "invalid cron: " + wf.Cron
			enabled = false
		} else {
			t := sched.Next(now)
			next = &t
		}
	}
	if err := s.st.SetWorkflowState(ctx, wf.ID, &now, lastErr, enabled, next); err != nil {
		log.Printf("scheduler: workflow %d state: %v", wf.ID, err)
	}
}

// Recover repairs schedules after a restart. Missed runs are never replayed.
func (s *Scheduler) Recover(ctx context.Context, now time.Time) error {
	rows, err := s.st.ListWorkflows(ctx)
	if err != nil {
		return err
	}
	for i := range rows {
		wf := rows[i].Workflow
		if !wf.Enabled {
			continue
		}
		switch wf.TriggerKind {
		case "cron":
			if wf.NextRunAt != nil && wf.NextRunAt.After(now) {
				continue
			}
			sched, err := cron.ParseStandard(wf.Cron)
			if err != nil {
				if err := s.st.SetWorkflowState(ctx, wf.ID, nil, "invalid cron: "+wf.Cron, false, nil); err != nil {
					return err
				}
				continue
			}
			t := sched.Next(now)
			if err := s.st.SetWorkflowState(ctx, wf.ID, nil, wf.LastError, true, &t); err != nil {
				return err
			}
		case "once":
			if wf.RunAt != nil && !wf.RunAt.After(now) {
				if err := s.st.SetWorkflowState(ctx, wf.ID, nil, "missed", false, nil); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
