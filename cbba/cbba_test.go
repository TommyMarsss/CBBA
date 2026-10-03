package cbba

import (
	"math/rand"
	"strings"
	"testing"
)

// ---- helpers --------------------------------------------------------------

func robotsAt(coords ...[2]float64) []Robot {
	out := make([]Robot, len(coords))
	for i, c := range coords {
		out[i] = Robot{ID: i, X: c[0], Y: c[1], Speed: 1}
	}
	return out
}

func countDrops(s *Sim) int {
	n := 0
	for _, r := range s.Rounds {
		n += r.Drops
	}
	return n
}

func roundsWithConflicts(s *Sim) int {
	n := 0
	for _, r := range s.Rounds {
		if len(r.Conflicts) > 0 {
			n++
		}
	}
	return n
}

func randomScenario(seed int64, nRobots, nTasks int) ([]Robot, []Task) {
	rng := rand.New(rand.NewSource(seed))
	robots := make([]Robot, nRobots)
	for i := range robots {
		robots[i] = Robot{ID: i, X: rng.Float64() * 100, Y: rng.Float64() * 100, Speed: 1}
	}
	tasks := make([]Task, nTasks)
	for i := range tasks {
		tasks[i] = Task{
			ID: i, X: rng.Float64() * 100, Y: rng.Float64() * 100,
			Reward: 20 + rng.Float64()*60, Duration: 1 + rng.Float64()*3,
		}
	}
	return robots, tasks
}

// ---- 1. bid conflict arbitration ------------------------------------------

// Two robots equidistant from a single task place identical bids; the
// lower robot ID must win and both must agree on the winner.
func TestBidTieBreakByRobotID(t *testing.T) {
	robots := robotsAt([2]float64{0, 0}, [2]float64{10, 0})
	tasks := []Task{{ID: 0, X: 5, Y: 0, Reward: 10, Duration: 1}}

	res, sim := Run(tasks, robots, Params{CostPerDist: 0}, UniformLoss(0), 1, 50, 3)

	if got := res.Assignment[0]; got != 0 {
		t.Fatalf("tie broken incorrectly: task 0 assigned to robot %d, want 0", got)
	}
	// Both robots must agree on the winner (no divergent beliefs).
	for _, a := range sim.agents {
		if a.Z[0] != 0 {
			t.Fatalf("robot %d believes winner is %d, want 0", a.Robot.ID, a.Z[0])
		}
	}
	// The conflict must actually have occurred and been resolved.
	if roundsWithConflicts(sim) == 0 {
		t.Fatalf("expected at least one round with a conflict before resolution")
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("conflicts remain after convergence: %+v", res.Conflicts)
	}
}

// A closer robot must outbid a farther one regardless of ID order.
func TestHigherBidWinsRegardlessOfID(t *testing.T) {
	robots := robotsAt([2]float64{0, 0}, [2]float64{4, 0}) // robot 1 much closer
	tasks := []Task{{ID: 0, X: 5, Y: 0, Reward: 10, Duration: 1}}

	res, sim := Run(tasks, robots, Params{CostPerDist: 0}, UniformLoss(0), 1, 50, 3)
	if got := res.Assignment[0]; got != 1 {
		t.Fatalf("higher bid should win: task 0 assigned to robot %d, want 1", got)
	}
	for _, a := range sim.agents {
		if a.Z[0] != 1 {
			t.Fatalf("robot %d believes winner is %d, want 1", a.Robot.ID, a.Z[0])
		}
	}
}

// ---- 2. convergence under packet loss -------------------------------------

func TestConvergenceUnderPacketLoss(t *testing.T) {
	robots, tasks := randomScenario(7, 6, 12)
	res, sim := Run(tasks, robots, Params{CostPerDist: 0.05}, UniformLoss(0.5), 7, 500, 3)

	if !res.Converged {
		t.Fatalf("did not converge under 50%% loss after %d rounds", res.Rounds)
	}
	if countDrops(sim) == 0 {
		t.Fatalf("expected message drops to have occurred")
	}
	if violations := Validate(res, len(tasks)); len(violations) > 0 {
		t.Fatalf("invalid final assignment: %v", violations)
	}
}

// Total blackout for the first few rounds must not deadlock the system:
// once communication resumes, the auction must still converge.
func TestConvergenceAfterBlackout(t *testing.T) {
	robots, tasks := randomScenario(11, 5, 10)
	blackout := func(round int) float64 {
		if round <= 5 {
			return 1.0 // every message lost
		}
		return 0.3
	}
	res, sim := Run(tasks, robots, Params{CostPerDist: 0.05}, blackout, 11, 500, 3)

	if !res.Converged {
		t.Fatalf("did not converge after 5-round blackout (rounds=%d)", res.Rounds)
	}
	if violations := Validate(res, len(tasks)); len(violations) > 0 {
		t.Fatalf("invalid final assignment: %v", violations)
	}
	// During the blackout no consensus information can flow: robots may
	// still bid locally (that is correct CBBA behavior), but no merge
	// updates or resets may occur.
	for _, r := range sim.Rounds[:5] {
		for _, ev := range r.Events {
			if ev.Kind == EvUpdate || ev.Kind == EvReset {
				t.Fatalf("consensus event during blackout round %d: %+v", r.Number, ev)
			}
		}
	}
}

// ---- 3. unassignable tasks are reported, not dropped ----------------------

func TestUnassignableTaskReported(t *testing.T) {
	robots := robotsAt([2]float64{0, 0}, [2]float64{10, 0})
	tasks := []Task{
		{ID: 0, X: 2, Y: 0, Reward: 50, Duration: 1},    // attractive
		{ID: 1, X: 900, Y: 900, Reward: 1, Duration: 1}, // hopeless: far + cheap
	}
	res, _ := Run(tasks, robots, Params{CostPerDist: 0.5}, UniformLoss(0), 1, 100, 3)

	if _, ok := res.Assignment[1]; ok {
		t.Fatalf("task 1 should not have been assigned")
	}
	if len(res.Unassignable) != 1 || res.Unassignable[0].TaskID != 1 {
		t.Fatalf("task 1 missing from unassignable report: %+v", res.Unassignable)
	}
	if res.Unassignable[0].BestRobot < 0 {
		t.Fatalf("expected a best robot in the report: %+v", res.Unassignable[0])
	}
	if res.Unassignable[0].BestBid > 0 {
		t.Fatalf("best bid for hopeless task should be non-positive: %+v", res.Unassignable[0])
	}
	if violations := Validate(res, len(tasks)); len(violations) > 0 {
		t.Fatalf("validation failed: %v", violations)
	}
}

func TestOutOfRangeTaskReported(t *testing.T) {
	robots := robotsAt([2]float64{0, 0})
	tasks := []Task{{ID: 0, X: 100, Y: 0, Reward: 1000, Duration: 1}}
	res, _ := Run(tasks, robots, Params{MaxRange: 50}, UniformLoss(0), 1, 50, 3)

	if len(res.Unassignable) != 1 {
		t.Fatalf("out-of-range task not reported: %+v", res.Unassignable)
	}
	if got := res.Unassignable[0].Reason; !strings.Contains(got, "out of range") {
		t.Fatalf("unexpected reason: %s", got)
	}
}

// ---- 4. large-scale conflict-freeness -------------------------------------

func TestLargeScaleConflictFree(t *testing.T) {
	robots, tasks := randomScenario(99, 30, 100)
	res, _ := Run(tasks, robots, Params{CostPerDist: 0.05, MaxBundle: 6},
		UniformLoss(0.15), 99, 1000, 3)

	if !res.Converged {
		t.Fatalf("large scenario did not converge (rounds=%d)", res.Rounds)
	}
	if violations := Validate(res, len(tasks)); len(violations) > 0 {
		t.Fatalf("large-scale assignment invalid: %v", violations)
	}
	// Bundle caps must be respected.
	counts := map[int]int{}
	for _, r := range res.Assignment {
		counts[r]++
	}
	for robot, n := range counts {
		if n > 6 {
			t.Fatalf("robot %d holds %d tasks, cap is 6", robot, n)
		}
	}
}

// ---- 5. determinism --------------------------------------------------------

func TestDeterministic(t *testing.T) {
	robots, tasks := randomScenario(5, 8, 20)
	res1, _ := Run(tasks, robots, Params{CostPerDist: 0.05}, UniformLoss(0.3), 5, 500, 3)
	res2, _ := Run(tasks, robots, Params{CostPerDist: 0.05}, UniformLoss(0.3), 5, 500, 3)

	if res1.Rounds != res2.Rounds {
		t.Fatalf("round counts differ: %d vs %d", res1.Rounds, res2.Rounds)
	}
	for task, owner := range res1.Assignment {
		if res2.Assignment[task] != owner {
			t.Fatalf("task %d assigned to %d in run 1, %d in run 2", task, owner, res2.Assignment[task])
		}
	}
}
