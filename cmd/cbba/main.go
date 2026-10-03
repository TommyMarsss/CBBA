// Command cbba runs a randomized CBBA task-allocation scenario and writes
// a self-contained HTML replay of the auction/consensus process.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"

	"github.com/TommyMarsss/CBBA/cbba"
)

func main() {
	out := flag.String("out", "report.html", "output HTML file")
	seed := flag.Int64("seed", 42, "random seed (scenario + message loss)")
	numRobots := flag.Int("robots", 6, "number of robots")
	numTasks := flag.Int("tasks", 14, "number of tasks")
	loss := flag.Float64("loss", 0.25, "per-message loss probability")
	maxRounds := flag.Int("max-rounds", 300, "round cap before declaring non-convergence")
	area := flag.Float64("area", 100, "side length of the square workspace")
	flag.Parse()

	rng := rand.New(rand.NewSource(*seed))
	robots := make([]cbba.Robot, *numRobots)
	for i := range robots {
		robots[i] = cbba.Robot{ID: i, X: rng.Float64() * *area, Y: rng.Float64() * *area, Speed: 1}
	}
	tasks := make([]cbba.Task, *numTasks)
	for i := range tasks {
		tasks[i] = cbba.Task{
			ID: i, X: rng.Float64() * *area, Y: rng.Float64() * *area,
			Reward: 20 + rng.Float64()*60, Duration: 1 + rng.Float64()*3,
		}
	}

	params := cbba.Params{CostPerDist: 0.05}
	result, sim := cbba.Run(tasks, robots, params, cbba.UniformLoss(*loss), *seed, *maxRounds, 3)

	fmt.Printf("converged: %v in %d rounds\n", result.Converged, result.Rounds)
	fmt.Printf("assigned %d/%d tasks\n", len(result.Assignment), len(tasks))
	for _, u := range result.Unassignable {
		fmt.Printf("  unassignable task %d: %s\n", u.TaskID, u.Reason)
	}
	if violations := cbba.Validate(result, len(tasks)); len(violations) > 0 {
		fmt.Fprintln(os.Stderr, "VALIDATION FAILED:")
		for _, v := range violations {
			fmt.Fprintln(os.Stderr, "  -", v)
		}
		os.Exit(1)
	}

	html := cbba.RenderHTML(sim, result)
	if err := os.WriteFile(*out, []byte(html), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write report:", err)
		os.Exit(1)
	}
	fmt.Printf("replay written to %s\n", *out)
}
