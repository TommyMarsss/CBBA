package cbba

import "fmt"

// Validate checks a finished result against the problem's two hard
// requirements and returns a list of violations (empty = clean):
//
//  1. Conflict-freeness: no task is owned by more than one robot.
//  2. Completeness: every task is either assigned to exactly one robot
//     or explicitly reported as unassignable — never silently dropped.
func Validate(res Result, numTasks int) []string {
	var violations []string
	owners := map[int][]int{}
	for task, robot := range res.Assignment {
		owners[task] = append(owners[task], robot)
	}
	for task, robots := range owners {
		if len(robots) > 1 {
			violations = append(violations,
				fmt.Sprintf("task %d assigned to multiple robots %v", task, robots))
		}
	}
	for _, c := range res.Conflicts {
		violations = append(violations,
			fmt.Sprintf("unresolved conflict on task %d between robots %v", c.Task, c.Robots))
	}
	reported := map[int]bool{}
	for _, u := range res.Unassignable {
		reported[u.TaskID] = true
	}
	for id := 0; id < numTasks; id++ {
		if _, assigned := res.Assignment[id]; !assigned && !reported[id] {
			violations = append(violations,
				fmt.Sprintf("task %d neither assigned nor reported unassignable", id))
		}
	}
	return violations
}
