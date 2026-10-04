package cbba

// Report is the outcome of validating a final allocation.
type Report struct {
	Conflicts    []Conflict // non-coop tasks claimed by >1 robot
	Unassignable []int      // tasks no robot can accept at a positive score
	Unassigned   []int      // assignable tasks that ended up unallocated
	OK           bool       // true iff no conflicts and no unexplained leftovers
}

// Validate checks an allocation for the two safety properties and one
// liveness property required of the assignment:
//
//   - conflict-freedom: no non-cooperative task appears in two bundles;
//   - explicit unassignability: a task that no robot can accept (best
//     insertion score <= 0 or travel budget exceeded) is reported, never
//     silently dropped;
//   - completeness: every remaining task is either assigned or listed
//     as Unassigned (capacity-limited leftovers).
func Validate(robots []Robot, tasks []Task, lambda float64, assignment map[int][]int) Report {
	if lambda == 0 {
		lambda = 1
	}
	tmap := make(map[int]Task, len(tasks))
	for _, t := range tasks {
		tmap[t.ID] = t
	}
	rmap := make(map[int]Robot, len(robots))
	for _, r := range robots {
		rmap[r.ID] = r
	}
	rep := Report{OK: true}

	// 1. Conflict check.
	claims := map[int][]int{}
	assigned := map[int]bool{}
	for rid, bundle := range assignment {
		for _, j := range bundle {
			assigned[j] = true
			if !tmap[j].Coop {
				claims[j] = append(claims[j], rid)
			}
		}
	}
	for j, rs := range claims {
		if len(rs) > 1 {
			rep.Conflicts = append(rep.Conflicts, Conflict{Task: j, Robots: rs})
		}
	}

	// 2. & 3. Classify every unassigned task.
	for _, t := range tasks {
		if assigned[t.ID] {
			continue
		}
		if !assignableByAnyone(robots, t, lambda) {
			rep.Unassignable = append(rep.Unassignable, t.ID)
		} else {
			rep.Unassigned = append(rep.Unassigned, t.ID)
		}
	}

	if len(rep.Conflicts) > 0 || len(rep.Unassigned) > 0 {
		rep.OK = false
	}
	return rep
}

// assignableByAnyone reports whether at least one robot could take the
// task at a positive score, starting from an empty bundle. This is the
// acceptance test used to distinguish "impossible task" from "lost the
// auction / capacity exhausted".
func assignableByAnyone(robots []Robot, t Task, lambda float64) bool {
	for _, r := range robots {
		if r.Capacity <= 0 {
			continue
		}
		d := dist(r.X, r.Y, t.X, t.Y)
		if r.MaxTravel > 0 && d > r.MaxTravel {
			continue
		}
		if t.Reward-lambda*d > 0 {
			return true
		}
	}
	return false
}
