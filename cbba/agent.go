package cbba

import (
	"math"
	"sort"
)

// bidEpsilon is the tolerance used when comparing bids for ties.
const bidEpsilon = 1e-9

// beats reports whether bidder outbids the current winner (winnerBid,
// winnerID): strictly higher bid wins, ties go to the lower robot ID.
// A task with no current winner requires the bid to clear minBid.
func beats(bid float64, bidder int, winnerBid float64, winnerID int, minBid float64) bool {
	if winnerID == NoWinner {
		return bid > minBid+bidEpsilon
	}
	if bid > winnerBid+bidEpsilon {
		return true
	}
	if math.Abs(bid-winnerBid) <= bidEpsilon {
		return bidder < winnerID
	}
	return false
}

// Agent is one robot's local view of the auction. All consensus state is
// kept in three vectors indexed by task (Y, Z) and by robot (S).
type Agent struct {
	Robot  Robot
	Bundle []int     // tasks won, in the order they were added
	Path   []int     // tasks in execution order
	Y      []float64 // highest known bid per task
	Z      []int     // known winner per task (robot ID or NoWinner)
	S      []float64 // last time info was heard directly/indirectly per robot

	sim *Sim
}

func newAgent(sim *Sim, r Robot) *Agent {
	a := &Agent{
		Robot: r,
		Y:     make([]float64, len(sim.Tasks)),
		Z:     make([]int, len(sim.Tasks)),
		S:     make([]float64, len(sim.Robots)),
		sim:   sim,
	}
	for i := range a.Z {
		a.Z[i] = NoWinner
	}
	for i := range a.S {
		a.S[i] = -1
	}
	a.S[a.Robot.ID] = 0
	return a
}

// pathScore evaluates a candidate execution order: discounted rewards
// minus travel cost. A path containing an out-of-range leg scores -inf.
func (a *Agent) pathScore(path []int) float64 {
	p := a.sim.Params
	t := 0.0
	x, y := a.Robot.X, a.Robot.Y
	score := 0.0
	for _, tid := range path {
		tk := a.sim.Tasks[tid]
		d := math.Hypot(tk.X-x, tk.Y-y)
		if p.MaxRange > 0 && d > p.MaxRange {
			return math.Inf(-1)
		}
		t += d / a.Robot.Speed
		score += tk.Reward * math.Pow(p.Discount, t)
		score -= p.CostPerDist * d
		t += tk.Duration
		x, y = tk.X, tk.Y
	}
	return score
}

func (a *Agent) inBundle(task int) bool {
	for _, t := range a.Bundle {
		if t == task {
			return true
		}
	}
	return false
}

// bestInsertion finds the path position with the highest marginal score
// for task j. Returns the position and the marginal gain.
func (a *Agent) bestInsertion(j int) (pos int, gain float64) {
	base := a.pathScore(a.Path)
	pos, gain = -1, math.Inf(-1)
	for p := 0; p <= len(a.Path); p++ {
		cand := make([]int, 0, len(a.Path)+1)
		cand = append(cand, a.Path[:p]...)
		cand = append(cand, j)
		cand = append(cand, a.Path[p:]...)
		if gainP := a.pathScore(cand) - base; gainP > gain {
			gain, pos = gainP, p
		}
	}
	return pos, gain
}

// buildBundle greedily adds tasks to the bundle until no winning bid is
// available or the bundle is full. Emits bid events.
func (a *Agent) buildBundle(round int) []Event {
	var evs []Event
	for len(a.Bundle) < a.sim.Params.MaxBundle {
		bestTask, bestPos := -1, -1
		bestGain := math.Inf(-1)
		for j := range a.sim.Tasks {
			if a.inBundle(j) {
				continue
			}
			pos, gain := a.bestInsertion(j)
			if gain <= bestGain {
				continue
			}
			if !beats(gain, a.Robot.ID, a.Y[j], a.Z[j], a.sim.Params.MinBid) {
				continue
			}
			bestTask, bestPos, bestGain = j, pos, gain
		}
		if bestTask < 0 {
			break
		}
		a.Bundle = append(a.Bundle, bestTask)
		a.Path = insertAt(a.Path, bestPos, bestTask)
		a.Y[bestTask] = bestGain
		a.Z[bestTask] = a.Robot.ID
		a.S[a.Robot.ID] = float64(round)
		ev := newEvent(EvBid, a.Robot.ID)
		ev.Task, ev.Bid, ev.Pos = bestTask, bestGain, bestPos
		ev.Reason = "outbids previous winner"
		evs = append(evs, ev)
	}
	return evs
}

func insertAt(s []int, pos, v int) []int {
	out := make([]int, 0, len(s)+1)
	out = append(out, s[:pos]...)
	out = append(out, v)
	out = append(out, s[pos:]...)
	return out
}

// Message is the gossip payload exchanged during consensus: a snapshot of
// one agent's winning-bid, winner and timestamp vectors.
type Message struct {
	Sender int
	Y      []float64
	Z      []int
	S      []float64
}

func (a *Agent) snapshot() Message {
	y := make([]float64, len(a.Y))
	copy(y, a.Y)
	z := make([]int, len(a.Z))
	copy(z, a.Z)
	s := make([]float64, len(a.S))
	copy(s, a.S)
	return Message{Sender: a.Robot.ID, Y: y, Z: z, S: s}
}

// merge applies the CBBA consensus rule table to a message received from
// agent k at the given round time. Timestamp ties and stale information
// are resolved by the per-robot freshness vector S.
func (a *Agent) merge(msg Message, round int) []Event {
	k := msg.Sender
	var evs []Event
	apply := func(ev Event, ok bool) {
		if ok {
			evs = append(evs, ev)
		}
	}
	for j := range a.sim.Tasks {
		zi, zk := a.Z[j], msg.Z[j]
		switch {
		case zk == k: // sender claims the task for itself
			switch {
			case zi == a.Robot.ID:
				if beats(msg.Y[j], k, a.Y[j], a.Robot.ID, a.sim.Params.MinBid) {
					apply(a.update(j, msg))
				}
			case zi == k || zi == NoWinner:
				apply(a.update(j, msg))
			default: // zi is a third robot m
				if msg.S[zi] > a.S[zi] || beats(msg.Y[j], k, a.Y[j], zi, a.sim.Params.MinBid) {
					apply(a.update(j, msg))
				}
			}
		case zk == a.Robot.ID: // sender believes the receiver wins
			switch {
			case zi == a.Robot.ID || zi == NoWinner:
				// agree; nothing to do
			case zi == k:
				apply(a.reset(j, msg))
			default: // zi is a third robot m
				if msg.S[zi] > a.S[zi] {
					apply(a.reset(j, msg))
				}
			}
		case zk == NoWinner:
			// Sender knows no winner. If the receiver's belief came from
			// the sender itself (zi == k) or from a third party m whose
			// info the sender has fresher, the belief is stale: reset.
			// This is how a winner's own reset propagates through gossip.
			switch {
			case zi == k:
				if msg.S[k] > a.S[k] {
					apply(a.reset(j, msg))
				}
			case zi == a.Robot.ID || zi == NoWinner:
				// leave
			default: // zi is a third robot m
				if msg.S[zi] > a.S[zi] {
					apply(a.reset(j, msg))
				}
			}
		default: // zk is a third robot m
			m := zk
			switch {
			case zi == a.Robot.ID:
				if msg.S[m] > a.S[m] && beats(msg.Y[j], m, a.Y[j], a.Robot.ID, a.sim.Params.MinBid) {
					apply(a.update(j, msg))
				}
			case zi == k:
				if msg.S[m] > a.S[m] {
					apply(a.update(j, msg))
				} else {
					apply(a.reset(j, msg))
				}
			case zi == m || zi == NoWinner:
				if msg.S[m] > a.S[m] {
					apply(a.update(j, msg))
				}
			default: // zi is a fourth robot n
				if msg.S[m] > a.S[m] {
					apply(a.update(j, msg))
				}
			}
		}
	}
	// Gossip the freshness vector: direct contact with k, plus everything
	// k has heard about other robots.
	a.S[k] = float64(round)
	for m := range a.S {
		if msg.S[m] > a.S[m] {
			a.S[m] = msg.S[m]
		}
	}
	return evs
}

// update adopts the sender's winner/bid for task j. If the local state
// is already identical (common when freshness timestamps trigger the
// rule but the values agree), it is a no-op and emits no event — only
// genuine state changes count as auction/consensus activity.
func (a *Agent) update(j int, msg Message) (Event, bool) {
	if a.Y[j] == msg.Y[j] && a.Z[j] == msg.Z[j] {
		return Event{}, false
	}
	old := a.Z[j]
	a.Y[j] = msg.Y[j]
	a.Z[j] = msg.Z[j]
	ev := newEvent(EvUpdate, a.Robot.ID)
	ev.Task, ev.From = j, msg.Sender
	ev.OldWinner, ev.NewWinner = old, msg.Z[j]
	ev.Reason = "adopted fresher/higher winning bid"
	return ev, true
}

// reset clears stale winner info for task j (same no-op rule as update).
func (a *Agent) reset(j int, msg Message) (Event, bool) {
	if a.Z[j] == NoWinner && a.Y[j] == 0 {
		return Event{}, false
	}
	old := a.Z[j]
	a.Y[j] = 0
	a.Z[j] = NoWinner
	ev := newEvent(EvReset, a.Robot.ID)
	ev.Task, ev.From = j, msg.Sender
	ev.OldWinner, ev.NewWinner = old, NoWinner
	ev.Reason = "sender proved receiver's winner info stale"
	return ev, true
}

// releaseLost removes bundle tasks the agent no longer wins (z_j != me)
// after consensus. Committed tasks (z_j == me) are kept: the auction is
// a committed-bid protocol — once the agent wins a task, it keeps it
// unless another robot outbids it. This is what guarantees convergence:
// winning bids are monotone non-decreasing, so no oscillation is
// possible. (The cascade release from the original CBBA paper, which
// resets the agent's own bids when a predecessor is lost, can livelock
// when marginals are path-dependent and complementary tasks exist.)
func (a *Agent) releaseLost(round int) []Event {
	var evs []Event
	kept := make([]int, 0, len(a.Bundle))
	for _, t := range a.Bundle {
		if a.Z[t] == a.Robot.ID {
			kept = append(kept, t)
			continue
		}
		ev := newEvent(EvRelease, a.Robot.ID)
		ev.Task = t
		ev.OldWinner, ev.NewWinner = a.Robot.ID, a.Z[t]
		ev.Reason = "outbid by another robot"
		evs = append(evs, ev)
	}
	if len(evs) == 0 {
		return nil
	}
	a.Bundle = kept
	path := make([]int, 0, len(a.Path))
	for _, t := range a.Path {
		if a.Z[t] == a.Robot.ID {
			path = append(path, t)
		}
	}
	a.Path = path
	a.S[a.Robot.ID] = float64(round)
	return evs
}

// findConflicts reports tasks appearing in more than one agent's bundle.
// The result is always non-nil so it marshals to [] (not null) in JSON.
func findConflicts(agents []*Agent) []Conflict {
	owners := map[int][]int{}
	for _, a := range agents {
		for _, t := range a.Bundle {
			owners[t] = append(owners[t], a.Robot.ID)
		}
	}
	out := []Conflict{}
	for task, rs := range owners {
		if len(rs) > 1 {
			sort.Ints(rs)
			out = append(out, Conflict{Task: task, Robots: rs})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Task < out[j].Task })
	return out
}
