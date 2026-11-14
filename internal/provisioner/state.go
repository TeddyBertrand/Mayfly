package provisioner

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/TeddyBertrand/Mayfly/internal/workflow"
)

// State is where a job's worker is in its lifecycle. docs/state-machine.md has the diagram.
type State int

// The worker states. Done and Dropped are final: the entry is removed once the worker is gone.
const (
	Waiting State = iota
	Provisioning
	Ready
	Running
	Done
	Dropped
)

var stateNames = [...]string{"waiting", "provisioning", "ready", "running", "done", "dropped"}

// String returns the lowercase state name used in logs.
func (s State) String() string {
	if s < 0 || int(s) >= len(stateNames) {
		return fmt.Sprintf("state(%d)", int(s))
	}
	return stateNames[s]
}

// transitions lists every allowed move; any other move is rejected.
var transitions = map[State][]State{
	Waiting:      {Provisioning, Dropped},
	Provisioning: {Ready, Dropped},
	Ready:        {Running, Done, Dropped},
	Running:      {Done},
}

// errUntracked means an event concerns no job this process is handling.
var errUntracked = errors.New("job not tracked")

// canTransition reports whether the state machine allows moving from one state to another.
func canTransition(from, to State) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// target returns the state a workflow_job action asks for, given the current state.
// A running job always ends as Done, even when cancelled, because its runner exits by itself.
func target(action workflow.Action, conclusion string, cur State) State {
	if action == workflow.ActionInProgress {
		return Running
	}
	if cur == Running || conclusion != workflow.ConclusionCancelled {
		return Done
	}
	return Dropped
}

// entry is the tracked state of one job's worker.
type entry struct {
	state  State
	runner string
	cancel context.CancelFunc
}

// tracker holds the state of every job this process is handling, in memory only.
type tracker struct {
	mu   sync.Mutex
	jobs map[int64]*entry
}

// newTracker returns an empty tracker.
func newTracker() *tracker {
	return &tracker{jobs: map[int64]*entry{}}
}

// add starts tracking id as Waiting, reporting false if it is already tracked.
func (t *tracker) add(id int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.jobs[id]; ok {
		return false
	}
	t.jobs[id] = &entry{state: Waiting}
	return true
}

// remove stops tracking id.
func (t *tracker) remove(id int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.jobs, id)
}

// state returns the current state of id and whether it is tracked.
func (t *tracker) state(id int64) (State, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.jobs[id]
	if !ok {
		return 0, false
	}
	return e.state, true
}

// begin moves id from Waiting to Provisioning and keeps cancel to stop its worker later.
// It fails if the job was dropped while it waited in the queue.
func (t *tracker) begin(id int64, cancel context.CancelFunc) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, err := t.move(id, Provisioning)
	if err != nil {
		return err
	}
	e.cancel = cancel
	return nil
}

// ready moves id from Provisioning to Ready and records its runner name.
// It fails if the job was dropped while its runner was being registered.
func (t *tracker) ready(id int64, runner string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, err := t.move(id, Ready)
	if err != nil {
		return err
	}
	e.runner = runner
	return nil
}

// update applies an in_progress or completed event to the entry it concerns, matched by runner name
// and then by job ID, since a runner may pick up a different job than the one it was created for.
// Dropping an entry cancels its worker. It returns the entry's job ID and its old and new state.
func (t *tracker) update(action workflow.Action, job workflow.Job) (id int64, from, to State, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	id, e := t.find(job)
	if e == nil {
		return job.ID, 0, 0, errUntracked
	}
	from = e.state
	to = target(action, job.Conclusion, from)
	if _, err := t.move(id, to); err != nil {
		return id, from, to, err
	}
	if to == Dropped && e.cancel != nil {
		e.cancel()
	}
	return id, from, to, nil
}

// find returns the entry whose runner is job.RunnerName, or else the entry for job.ID. Callers hold mu.
func (t *tracker) find(job workflow.Job) (int64, *entry) {
	if job.RunnerName != "" {
		for id, e := range t.jobs {
			if e.runner == job.RunnerName {
				return id, e
			}
		}
	}
	return job.ID, t.jobs[job.ID]
}

// move sets id to state to if the state machine allows it. Callers hold mu.
func (t *tracker) move(id int64, to State) (*entry, error) {
	e, ok := t.jobs[id]
	if !ok {
		return nil, errUntracked
	}
	if !canTransition(e.state, to) {
		return nil, fmt.Errorf("transition %s -> %s not allowed", e.state, to)
	}
	e.state = to
	return e, nil
}
