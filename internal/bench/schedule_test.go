package bench

import (
	"reflect"
	"testing"
)

// TestSchedule checks the round-robin order and which iterations are warm-up.
func TestSchedule(t *testing.T) {
	got := Schedule([]string{"go", "npm"}, []string{"hosted", "mayfly"}, 1, 1)
	want := []Item{
		{Iteration: 0, Workflow: "go", Runner: "hosted", Warmup: true},
		{Iteration: 0, Workflow: "go", Runner: "mayfly", Warmup: true},
		{Iteration: 0, Workflow: "npm", Runner: "hosted", Warmup: true},
		{Iteration: 0, Workflow: "npm", Runner: "mayfly", Warmup: true},
		{Iteration: 1, Workflow: "go", Runner: "hosted"},
		{Iteration: 1, Workflow: "go", Runner: "mayfly"},
		{Iteration: 1, Workflow: "npm", Runner: "hosted"},
		{Iteration: 1, Workflow: "npm", Runner: "mayfly"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Schedule =\n%+v\nwant\n%+v", got, want)
	}
}

// TestScheduleEmpty checks that no runs, workflows or runners give an empty schedule.
func TestScheduleEmpty(t *testing.T) {
	for _, s := range [][]Item{
		Schedule(nil, []string{"hosted"}, 1, 1),
		Schedule([]string{"go"}, nil, 1, 1),
		Schedule([]string{"go"}, []string{"hosted"}, 0, 0),
	} {
		if len(s) != 0 {
			t.Errorf("want empty schedule, got %+v", s)
		}
	}
}
