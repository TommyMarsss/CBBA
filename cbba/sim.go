package cbba

import (
	"fmt"
	"math/rand"
	"sort"
)

// Sim drives the auction/consensus rounds and records the replay log.
type Sim struct {
	Tasks  []Task
	Robots []Robot
	Params Params

	agents []*Agent
	rng    *rand.Rand
	loss   func(round int) float64

	Rounds []RoundLog
}

// New creates a simulator over the given tasks and robots.
// lossRate maps a round number to the per-message loss probability for
// that round (use a constant function for uniform loss). The RNG is
// seeded, so identical inputs produce identical runs.
func New(tasks []Task, robots []Robot, params Params, lossRate func(round int) float64, seed int64) *Sim {
	s := &Sim{
		Tasks:  tasks,
		Robots: robots,
		Params: params.withDefaults(len(tasks)),
		loss:   lossRate,
		rng:    rand.New(rand.NewSource(seed)),
	}
	for _, r := range robots {
		s.agents = append(s.agents, newAgent(s, r))
	}
	return s
}

// Agents exposes the final agent states (read-only convenience for
// tests, debugging and result inspection).
func (s *Sim) Agents() []*Agent { return s.agents }

// UniformLoss returns a loss function with a constant per-message drop
// probability.
func UniformLoss(p float64) func(int) float64 {
	return func(int) float64 { return p }
}

// Run executes rounds until convergence or maxRounds is hit.
//
// Convergence criterion (a centralized oracle used only to terminate the
// simulation, never to influence the assignment): the auction has been
// quiet (no bids, consensus updates or releases) for quietRounds
// consecutive rounds, there are no unresolved conflicts, and every
// agent's (Y, Z, S) state is a fixed point of the pairwise merge with
// every other agent — i.e. no message that could be delivered would
// change anyone's state. This is what guarantees the final assignment
// is conflict-free even though earlier rounds may have lost messages.
func Run(tasks []Task, robots []Robot, params Params, lossRate func(round int) float64, seed int64, maxRounds int, quietRounds int) (Result, *Sim) {
	s := New(tasks, robots, params, lossRate, seed)
	quiet := 0
	converged := false
	for round := 1; round <= maxRounds; round++ {
		log := s.step(round, lossRate(round))
		s.Rounds = append(s.Rounds, log)
		// Message drops are recorded for the replay but do not count as
		// auction/consensus activity — otherwise any nonzero loss rate
		// would make the quiet streak unreachable.
		active := false
		for _, ev := range log.Events {
			if ev.Kind != EvDrop {
				active = true
				break
			}
		}
		if !active {
			quiet++
		} else {
			quiet = 0
		}
		if quiet >= quietRounds && len(findConflicts(s.agents)) == 0 && s.consistent() {
			converged = true
			break
		}
	}
	return s.finish(converged, len(s.Rounds)), s
}

// consistent reports whether every agent's state is a fixed point of the
// pairwise consensus merge with every other agent. It dry-runs each
// merge on a throwaway copy; the real agents are untouched.
func (s *Sim) consistent() bool {
	snaps := make([]Message, len(s.agents))
	for i, a := range s.agents {
		snaps[i] = a.snapshot()
	}
	for _, a := range s.agents {
		probe := &Agent{
			Robot: a.Robot,
			Y:     append([]float64(nil), a.Y...),
			Z:     append([]int(nil), a.Z...),
			S:     append([]float64(nil), a.S...),
			sim:   s,
		}
		for k := range s.agents {
			if k == a.Robot.ID {
				continue
			}
			if len(probe.merge(snaps[k], 0)) > 0 {
				return false
			}
		}
	}
	return true
}

// step executes one full round: bundle construction, gossip with the
// given per-message loss probability, cascade release.
func (s *Sim) step(round int, loss float64) RoundLog {
	log := RoundLog{Number: round, Events: []Event{}}

	// Phase 1: each agent extends its bundle.
	for _, a := range s.agents {
		log.Events = append(log.Events, a.buildBundle(round)...)
	}
	// Conflicts are captured right after bidding, before consensus
	// resolves them — this is what the replay highlights.
	log.Conflicts = findConflicts(s.agents)

	// Phase 2: gossip. Messages are snapshots taken at the phase
	// boundary, so all agents observe a consistent view of the round.
	snaps := make([]Message, len(s.agents))
	for i, a := range s.agents {
		snaps[i] = a.snapshot()
	}
	for i, a := range s.agents {
		for k := range s.agents {
			if i == k {
				continue
			}
			if s.rng.Float64() < loss {
				log.Drops++
				ev := newEvent(EvDrop, -1)
				ev.From, ev.To = k, i
				ev.Reason = "message lost in transit"
				log.Events = append(log.Events, ev)
				continue
			}
			log.Events = append(log.Events, a.merge(snaps[k], round)...)
		}
	}

	// Phase 3: release tasks lost during consensus.
	for _, a := range s.agents {
		log.Events = append(log.Events, a.releaseLost(round)...)
	}

	log.Bundles = make([][]int, len(s.agents))
	for i, a := range s.agents {
		log.Bundles[i] = append([]int{}, a.Bundle...)
	}
	return log
}

// finish assembles the final result: assignment, conflicts, and an
// explicit report of tasks no robot could take on.
func (s *Sim) finish(converged bool, rounds int) Result {
	assignment := map[int]int{}
	for _, a := range s.agents {
		for _, t := range a.Bundle {
			assignment[t] = a.Robot.ID
		}
	}
	res := Result{
		Converged:    converged,
		Rounds:       rounds,
		Assignment:   assignment,
		Conflicts:    findConflicts(s.agents),
		Unassignable: []UnassignableTask{},
	}
	for _, tk := range s.Tasks {
		if _, ok := assignment[tk.ID]; !ok {
			res.Unassignable = append(res.Unassignable, s.explainUnassignable(tk.ID))
		}
	}
	sort.Slice(res.Unassignable, func(i, j int) bool {
		return res.Unassignable[i].TaskID < res.Unassignable[j].TaskID
	})
	return res
}

// explainUnassignable computes, for an unassigned task, the best marginal
// bid any robot could still offer (against its final path) and why the
// task was rejected.
func (s *Sim) explainUnassignable(task int) UnassignableTask {
	bestBid := -1e300
	bestRobot := -1
	allOutOfRange := true
	for _, a := range s.agents {
		pos, gain := a.bestInsertion(task)
		if pos < 0 {
			continue // unreachable for this robot
		}
		allOutOfRange = false
		if gain > bestBid {
			bestBid, bestRobot = gain, a.Robot.ID
		}
	}
	reason := ""
	switch {
	case allOutOfRange:
		reason = fmt.Sprintf("out of range: no robot can reach task within MaxRange %.2f", s.Params.MaxRange)
	case bestRobot < 0:
		reason = "no feasible insertion for any robot"
	default:
		reason = fmt.Sprintf("best marginal bid %.4f by robot %d does not exceed minBid %.4f",
			bestBid, bestRobot, s.Params.MinBid)
	}
	return UnassignableTask{
		TaskID:    task,
		Reason:    reason,
		BestBid:   bestBid,
		BestRobot: bestRobot,
	}
}
