package cbba

import "sort"

// Agent is the per-robot auction state. Every robot runs the same code
// and only talks to its neighbours through Message values, so the
// simulation faithfully mirrors a distributed deployment.
type Agent struct {
	Robot
	Lambda float64 // travel-cost weight in the scoring function

	bundle []int           // ordered list of task IDs this robot plans to execute
	y      map[int]float64 // y[j] = highest winning bid for task j known to me
	z      map[int]int     // z[j] = winner of task j known to me (noWinner if none)
	s      map[int]int     // s[r] = latest round I heard (directly or not) from robot r

	changed bool // set whenever state mutates; used for convergence detection
}

func newAgent(r Robot, lambda float64, nRobots int) *Agent {
	a := &Agent{
		Robot:  r,
		Lambda: lambda,
		y:      make(map[int]float64),
		z:      make(map[int]int),
		s:      make(map[int]int),
	}
	a.s[r.ID] = 0
	return a
}

// Message is the only data exchanged between robots: the sender's
// winning-bid vector, winner vector and timestamp vector.
type Message struct {
	From  int
	Round int
	Y     map[int]float64
	Z     map[int]int
	S     map[int]int
}

func (a *Agent) message(round int) Message {
	y := make(map[int]float64, len(a.y))
	for k, v := range a.y {
		y[k] = v
	}
	z := make(map[int]int, len(a.z))
	for k, v := range a.z {
		z[k] = v
	}
	s := make(map[int]int, len(a.s))
	for k, v := range a.s {
		s[k] = v
	}
	a.s[a.ID] = round // timestamp of my own latest information
	s[a.ID] = round
	return Message{From: a.ID, Round: round, Y: y, Z: z, S: s}
}

// pathLength returns the travel distance of the robot's current bundle
// (start at the robot, visit tasks in bundle order).
func (a *Agent) pathLength(tasks map[int]Task, bundle []int) float64 {
	x, y := a.X, a.Y
	total := 0.0
	for _, id := range bundle {
		t := tasks[id]
		total += dist(x, y, t.X, t.Y)
		x, y = t.X, t.Y
	}
	return total
}

// bestInsertion finds the bundle position with the minimum added travel
// for task t. It returns the position, the marginal distance and whether
// the insertion respects the robot's travel budget.
func (a *Agent) bestInsertion(tasks map[int]Task, t Task) (pos int, delta float64, ok bool) {
	cur := a.pathLength(tasks, a.bundle)
	best := -1
	bestDelta := 0.0
	for p := 0; p <= len(a.bundle); p++ {
		nb := make([]int, 0, len(a.bundle)+1)
		nb = append(nb, a.bundle[:p]...)
		nb = append(nb, t.ID)
		nb = append(nb, a.bundle[p:]...)
		d := a.pathLength(tasks, nb) - cur
		if best == -1 || d < bestDelta {
			best, bestDelta = p, d
		}
	}
	if a.MaxTravel > 0 && cur+bestDelta > a.MaxTravel {
		return 0, 0, false
	}
	return best, bestDelta, true
}

// scoreOf computes the bid this robot would place on task t:
// reward minus the weighted marginal travel cost of the best insertion.
func (a *Agent) scoreOf(tasks map[int]Task, t Task) (pos int, bid float64, ok bool) {
	pos, delta, ok := a.bestInsertion(tasks, t)
	if !ok {
		return 0, 0, false
	}
	return pos, t.Reward - a.Lambda*delta, true
}

// buildBundle is CBBA phase 1: greedily append the locally best task
// until the bundle is full or no task yields a positive, winning bid.
// Events are appended to ev for the replay log.
func (a *Agent) buildBundle(tasks []Task, tmap map[int]Task, round int, ev *[]Event) {
	for len(a.bundle) < a.Capacity {
		bestTask, bestPos := -1, 0
		bestBid := 0.0
		for _, t := range tasks {
			if containsInt(a.bundle, t.ID) {
				continue
			}
			pos, bid, ok := a.scoreOf(tmap, t)
			if !ok || bid <= 0 {
				continue
			}
			// Only bid when the current winner is me (re-insertion after
			// a prune) or when I would beat the winner I believe in.
			if w := winnerOr(a.z, t.ID); !t.Coop && w != a.ID && !wins(bid, a.ID, a.y[t.ID], w) {
				continue
			}
			if bestTask == -1 || bid > bestBid {
				bestTask, bestPos, bestBid = t.ID, pos, bid
			}
		}
		if bestTask == -1 {
			break
		}
		a.bundle = append(a.bundle, 0)
		copy(a.bundle[bestPos+1:], a.bundle[bestPos:])
		a.bundle[bestPos] = bestTask
		if !tmap[bestTask].Coop {
			a.y[bestTask] = bestBid
			a.z[bestTask] = a.ID
		}
		a.changed = true
		*ev = append(*ev, Event{
			Round: round, Type: EvBid, Robot: a.ID, Task: bestTask, Bid: bestBid,
			Note: "插入位置 " + itoa(bestPos),
		})
	}
	// Release claims I can no longer honor: I believe I win task j, but j
	// is not in my bundle (capacity full or no profitable insertion).
	// Holding such a claim would block every other robot from the task.
	var stale []int
	for j, w := range a.z {
		if w == a.ID && !containsInt(a.bundle, j) {
			stale = append(stale, j)
		}
	}
	sort.Ints(stale)
	for _, j := range stale {
		delete(a.y, j)
		delete(a.z, j)
		a.changed = true
		*ev = append(*ev, Event{
			Round: round, Type: EvRelease, Robot: a.ID, Task: j,
			Note: "无法承接，释放主张",
		})
	}
}

// receive merges one incoming message into the local belief state
// (CBBA phase 2). The decision rules follow the consensus table of the
// original paper, reduced to the four sender-claim cases. Events are
// recorded for replay; changed flags drive convergence detection.
func (a *Agent) receive(m Message, tasks []Task, tmap map[int]Task, round int, ev *[]Event) {
	for _, t := range tasks {
		j := t.ID
		if t.Coop {
			continue // cooperative tasks are not exclusively claimed
		}
		zk, yk := winnerOr(m.Z, j), m.Y[j]
		zi, yi := winnerOr(a.z, j), a.y[j]
		switch {
		case zk == m.From:
			// Sender claims the task itself.
			switch {
			case zi == m.From:
				// Sender's own claim is authoritative, but only when it
				// is newer than what I last heard from the sender.
				if sOr(m.S, m.From) > sOr(a.s, m.From) {
					a.update(j, yk, zk, round, ev)
				}
			case zi == a.ID:
				if wins(yk, zk, yi, zi) {
					a.update(j, yk, zk, round, ev)
				}
			case zi == noWinner:
				a.update(j, yk, zk, round, ev)
			default: // I believe a third party wins
				if wins(yk, zk, yi, zi) {
					a.update(j, yk, zk, round, ev)
				}
			}
		case zk == a.ID:
			// Sender thinks I win. My own bids are computed locally, so
			// the only actionable information here is the sender's
			// retraction of its own earlier claim.
			if zi == m.From {
				delete(a.y, j)
				delete(a.z, j)
				a.changed = true
				*ev = append(*ev, Event{
					Round: round, Type: EvReset, Robot: a.ID, Task: j, Other: m.From,
					Note: "发送者已放弃，重置其过期主张",
				})
			}
		case zk == noWinner:
			// Sender released the task: only the winner itself may reset.
			if zi == m.From {
				delete(a.y, j)
				delete(a.z, j)
				a.changed = true
				*ev = append(*ev, Event{
					Round: round, Type: EvReset, Robot: a.ID, Task: j, Other: m.From,
					Note: "接受胜者的释放",
				})
			}
		default:
			// Sender thinks a third robot wins.
			switch {
			case zi == m.From:
				// Sender overrode its own earlier claim: always follow.
				a.update(j, yk, zk, round, ev)
			default:
				// Third-party claims (adopt / refresh / switch / dethrone)
				// are only considered when the sender heard from the
				// claimed winner more recently than I did. Without this
				// freshness gate a stale claim circulates in the
				// population forever: the winner's reset and someone's
				// stale relay keep resurrecting each other.
				if sOr(m.S, zk) <= sOr(a.s, zk) {
					continue
				}
				switch {
				case zi == noWinner:
					a.update(j, yk, zk, round, ev)
				case zi == zk:
					a.update(j, yk, zk, round, ev)
				default:
					if wins(yk, zk, yi, zi) {
						a.update(j, yk, zk, round, ev)
					}
				}
			}
		}
	}
	// Merge timestamp vectors (used for diagnostics and fidelity with
	// the original protocol; convergence itself is detected via the
	// changed flags in this synchronous simulator).
	for r, ts := range m.S {
		if ts > sOr(a.s, r) {
			a.s[r] = ts
		}
	}
	if m.Round > sOr(a.s, m.From) {
		a.s[m.From] = m.Round
	}
}

// update records a new winning bid/winner for task j and emits a
// resolve event when the robot loses a task it believed it owned.
func (a *Agent) update(j int, bid float64, winner, round int, ev *[]Event) {
	prev := winnerOr(a.z, j)
	if prev == winner && almostEq(a.y[j], bid) {
		return
	}
	if prev == a.ID && winner != a.ID {
		*ev = append(*ev, Event{
			Round: round, Type: EvResolve, Robot: a.ID, Task: j, Other: winner, Bid: bid,
			Note: "失去任务，让位给更高出价",
		})
	}
	a.y[j] = bid
	a.z[j] = winner
	a.changed = true
}

// pruneBundle drops every bundle task the robot no longer believes it
// wins, plus all tasks inserted after the first lost one (their marginal
// bids were computed against a path that no longer exists). Cooperative
// tasks are never claimed exclusively and are kept.
func (a *Agent) pruneBundle(tmap map[int]Task, round int, ev *[]Event) {
	for i, id := range a.bundle {
		if tmap[id].Coop || winnerOr(a.z, id) == a.ID {
			continue
		}
		for _, dropped := range a.bundle[i:] {
			*ev = append(*ev, Event{
				Round: round, Type: EvRelease, Robot: a.ID, Task: dropped,
				Note: "从任务包中移除",
			})
		}
		a.bundle = a.bundle[:i]
		a.changed = true
		return
	}
}

func winnerOr(z map[int]int, j int) int {
	if w, ok := z[j]; ok {
		return w
	}
	return noWinner
}

// sOr returns the timestamp of the latest information known from robot r,
// or -1 if nothing was ever heard from it.
func sOr(s map[int]int, r int) int {
	if v, ok := s[r]; ok {
		return v
	}
	return -1
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func almostEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [8]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		buf[p] = '-'
	}
	return string(buf[p:])
}
