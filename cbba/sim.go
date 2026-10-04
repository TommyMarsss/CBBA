package cbba

import (
	"math/rand"
	"sort"
)

// EventType classifies replay-log events.
type EventType string

const (
	EvBid      EventType = "bid"      // robot placed a winning bid and inserted the task
	EvResolve  EventType = "resolve"  // robot lost a task it owned to a higher bid
	EvRelease  EventType = "release"  // robot dropped a task from its bundle
	EvReset    EventType = "reset"    // robot accepted the winner's release of a task
	EvConflict EventType = "conflict" // >=2 robots believed they owned the same task
	EvDrop     EventType = "drop"     // a consensus message was lost in transit
)

// Event is one replayable step of the auction/consensus process.
type Event struct {
	Round int
	Type  EventType
	Robot int
	Task  int
	Other int // other robot involved (conflict/drop/resolve), -1 if none
	Bid   float64
	Note  string
}

// RobotState is a snapshot of one robot's belief at the end of a round.
type RobotState struct {
	ID      int
	Bundle  []int
	Winners map[int]int     // task -> believed winner
	Bids    map[int]float64 // task -> believed winning bid
}

// RoundLog captures everything that happened in one consensus round.
type RoundLog struct {
	Round  int
	States []RobotState
	Events []Event
}

// LossFunc reports whether the message from -> to is dropped in a round.
type LossFunc func(round, from, to int) bool

// Config configures one CBBA run.
type Config struct {
	Robots    []Robot
	Tasks     []Task
	Lambda    float64  // travel-cost weight; 0 defaults to 1
	MaxRounds int      // safety cap; 0 defaults to 50*(robots+tasks)
	Loss      LossFunc // nil means a perfect channel
}

// Result is the outcome of a run: the final allocation, the convergence
// flag, the explicit report of tasks nobody could take, and the full
// replay log.
type Result struct {
	Assignment   map[int][]int // robot ID -> ordered task IDs
	Unassignable []int         // tasks no robot can accept at a positive score
	Unassigned   []int         // assignable tasks left over (e.g. capacity limits)
	Conflicts    []Conflict    // final-state conflicts (must be empty on success)
	Converged    bool
	Rounds       int
	Log          []RoundLog
}

// Conflict records a task claimed by more than one robot.
type Conflict struct {
	Task   int
	Robots []int
}

// RandomLoss returns a LossFunc that drops each message with
// probability p during rounds [0, untilRound) and is lossless
// afterwards. Deterministic for a given seed.
func RandomLoss(seed int64, p float64, untilRound int) LossFunc {
	rng := rand.New(rand.NewSource(seed))
	drops := make(map[[3]int]bool)
	return func(round, from, to int) bool {
		if round >= untilRound {
			return false
		}
		k := [3]int{round, from, to}
		if d, ok := drops[k]; ok {
			return d
		}
		d := rng.Float64() < p
		drops[k] = d
		return d
	}
}

// BlackoutLoss drops every message during rounds [0, untilRound).
func BlackoutLoss(untilRound int) LossFunc {
	return func(round, from, to int) bool { return round < untilRound }
}

// Run executes the auction until convergence (a full round with no
// state change and no lost messages) or until MaxRounds is reached.
func Run(cfg Config) *Result {
	lambda := cfg.Lambda
	if lambda == 0 {
		lambda = 1
	}
	maxRounds := cfg.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 50 * (len(cfg.Robots) + len(cfg.Tasks) + 1)
	}
	tmap := make(map[int]Task, len(cfg.Tasks))
	for _, t := range cfg.Tasks {
		tmap[t.ID] = t
	}
	agents := make([]*Agent, len(cfg.Robots))
	for i, r := range cfg.Robots {
		agents[i] = newAgent(r, lambda, len(cfg.Robots))
	}

	res := &Result{Assignment: make(map[int][]int)}
	converged := false
	round := 0
	for ; round < maxRounds; round++ {
		ev := []Event{}
		// Phase 1: bundle construction.
		for _, a := range agents {
			a.changed = false
		}
		for _, a := range agents {
			a.buildBundle(cfg.Tasks, tmap, round, &ev)
		}
		// Phase 2: broadcast + consensus.
		msgs := make([]Message, len(agents))
		for i, a := range agents {
			msgs[i] = a.message(round)
		}
		dropped := false
		for i := range agents {
			for j := range agents {
				if i == j {
					continue
				}
				if cfg.Loss != nil && cfg.Loss(round, agents[i].ID, agents[j].ID) {
					dropped = true
					ev = append(ev, Event{
						Round: round, Type: EvDrop, Robot: agents[i].ID, Other: agents[j].ID,
						Note: "共识消息丢失",
					})
					continue
				}
				agents[j].receive(msgs[i], cfg.Tasks, tmap, round, &ev)
			}
		}
		for _, a := range agents {
			a.pruneBundle(tmap, round, &ev)
		}
		// Detect residual belief conflicts for the replay log.
		ev = append(ev, detectConflicts(agents, tmap, round)...)
		// Snapshot the round.
		rl := RoundLog{Round: round, Events: ev}
		for _, a := range agents {
			st := RobotState{
				ID:      a.ID,
				Bundle:  append([]int(nil), a.bundle...),
				Winners: map[int]int{},
				Bids:    map[int]float64{},
			}
			for _, t := range cfg.Tasks {
				if w := winnerOr(a.z, t.ID); w != noWinner {
					st.Winners[t.ID] = w
					st.Bids[t.ID] = a.y[t.ID]
				}
			}
			rl.States = append(rl.States, st)
		}
		res.Log = append(res.Log, rl)
		// Convergence: a clean round (nothing changed, nothing dropped).
		changed := false
		for _, a := range agents {
			if a.changed {
				changed = true
				break
			}
		}
		if !changed && !dropped {
			converged = true
			break
		}
	}
	res.Converged = converged
	res.Rounds = round + 1
	if !converged {
		res.Rounds = maxRounds
	}
	for _, a := range agents {
		res.Assignment[a.ID] = append([]int(nil), a.bundle...)
	}
	rep := Validate(cfg.Robots, cfg.Tasks, lambda, res.Assignment)
	res.Conflicts = rep.Conflicts
	res.Unassignable = rep.Unassignable
	res.Unassigned = rep.Unassigned
	return res
}

// detectConflicts compares every pair of agents' winner beliefs and
// emits one event per task claimed by more than one robot.
func detectConflicts(agents []*Agent, tmap map[int]Task, round int) []Event {
	claims := map[int][]int{}
	for _, a := range agents {
		for j, w := range a.z {
			if w == a.ID && !tmap[j].Coop {
				claims[j] = append(claims[j], a.ID)
			}
		}
	}
	var ev []Event
	ids := make([]int, 0, len(claims))
	for j := range claims {
		ids = append(ids, j)
	}
	sort.Ints(ids)
	for _, j := range ids {
		rs := claims[j]
		if len(rs) < 2 {
			continue
		}
		for k := 1; k < len(rs); k++ {
			ev = append(ev, Event{
				Round: round, Type: EvConflict, Robot: rs[0], Other: rs[k], Task: j,
				Note: "两个机器人都认为自己赢得该任务",
			})
		}
	}
	return ev
}
