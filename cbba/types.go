// Package cbba implements the Consensus-Based Bundle Algorithm (CBBA)
// for decentralized multi-robot task allocation, together with a
// discrete-round simulator that can model message loss and record a
// full replay log of the auction/consensus process.
//
// The algorithm has two phases per round:
//
//  1. Bundle construction: each robot greedily inserts tasks into its
//     path at the position that maximizes its marginal score, bidding
//     only when its marginal score beats the currently known winning bid.
//  2. Consensus: robots exchange their winning-bid / winner / timestamp
//     vectors and merge them with the CBBA rule table. Losing robots
//     release the contested task and everything added after it.
//
// Conflict arbitration is total and deterministic: higher bid wins,
// ties are broken by lower robot ID.
package cbba

// NoWinner marks a task that currently has no known winning bidder.
const NoWinner = -1

// Task is a unit of work located at (X, Y). Reward is earned (discounted
// by arrival time) when the task is executed; Duration is the on-site
// service time.
type Task struct {
	ID       int     `json:"id"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Reward   float64 `json:"reward"`
	Duration float64 `json:"duration"`
}

// Robot is an auction participant starting at (X, Y).
type Robot struct {
	ID    int     `json:"id"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Speed float64 `json:"speed"`
}

// Params controls scoring and bidding.
type Params struct {
	// Discount is the per-unit-time reward discount factor in (0, 1].
	// Zero selects the default 0.99.
	Discount float64 `json:"discount"`
	// CostPerDist is subtracted from the score per unit of travel.
	CostPerDist float64 `json:"costPerDist"`
	// MinBid is the minimum marginal score required to place a bid.
	MinBid float64 `json:"minBid"`
	// MaxRange, if > 0, forbids any single travel leg longer than this.
	MaxRange float64 `json:"maxRange"`
	// MaxBundle caps the number of tasks one robot may hold.
	// Zero or negative means "number of tasks" (uncapped).
	MaxBundle int `json:"maxBundle"`
}

func (p *Params) withDefaults(numTasks int) Params {
	q := *p
	if q.Discount == 0 {
		q.Discount = 0.99
	}
	if q.MaxBundle <= 0 {
		q.MaxBundle = numTasks
	}
	return q
}

// EventKind classifies replay-log events.
type EventKind string

const (
	EvBid     EventKind = "bid"     // robot placed a winning bid on a task
	EvRelease EventKind = "release" // robot dropped tasks it no longer wins
	EvUpdate  EventKind = "update"  // consensus: adopted sender's winner/bid
	EvReset   EventKind = "reset"   // consensus: cleared stale winner info
	EvDrop    EventKind = "drop"    // message lost in transit
)

// Event is a single replay-log entry. Unused fields are -1 / "".
type Event struct {
	Kind      EventKind `json:"kind"`
	Robot     int       `json:"robot"`     // actor (bidder / releaser / merge receiver)
	Task      int       `json:"task"`      // task concerned, -1 if none
	Bid       float64   `json:"bid"`       // bid value for EvBid
	Pos       int       `json:"pos"`       // path insertion position for EvBid
	From      int       `json:"from"`      // message sender (EvUpdate/EvReset/EvDrop)
	To        int       `json:"to"`        // message receiver (EvDrop) == Robot otherwise
	OldWinner int       `json:"oldWinner"` // winner before the merge
	NewWinner int       `json:"newWinner"` // winner after the merge
	Reason    string    `json:"reason"`    // human-readable explanation
}

func newEvent(kind EventKind, robot int) Event {
	return Event{
		Kind: kind, Robot: robot,
		Task: -1, Pos: -1, From: -1, To: -1,
		OldWinner: NoWinner, NewWinner: NoWinner,
	}
}

// Conflict records a task owned by more than one robot at a round boundary.
type Conflict struct {
	Task   int   `json:"task"`
	Robots []int `json:"robots"`
}

// RoundLog captures everything observable at the end of one round.
type RoundLog struct {
	Number    int        `json:"number"`
	Events    []Event    `json:"events"`
	Bundles   [][]int    `json:"bundles"` // bundle of each robot, indexed by robot ID
	Conflicts []Conflict `json:"conflicts"`
	Drops     int        `json:"drops"`
}

// UnassignableTask is a task no robot could take on at an acceptable cost.
type UnassignableTask struct {
	TaskID    int     `json:"taskId"`
	Reason    string  `json:"reason"`
	BestBid   float64 `json:"bestBid"`
	BestRobot int     `json:"bestRobot"` // -1 if no robot could even reach it
}

// Result is the outcome of a simulation run.
type Result struct {
	Converged    bool               `json:"converged"`
	Rounds       int                `json:"rounds"`
	Assignment   map[int]int        `json:"assignment"` // task ID -> robot ID
	Unassignable []UnassignableTask `json:"unassignable"`
	Conflicts    []Conflict         `json:"conflicts"` // non-empty only if NOT converged cleanly
}
