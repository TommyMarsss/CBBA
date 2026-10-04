// Command cbba runs a CBBA task-allocation scenario and writes a single
// self-contained static HTML report that replays the auction and
// consensus rounds in the browser.
//
// Usage:
//
//	go run ./cmd/cbba [-robots N] [-tasks M] [-seed S] [-loss P] [-loss-rounds K] [-out report.html]
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"

	"github.com/TommyMarsss/CBBA/cbba"
)

func main() {
	nRobots := flag.Int("robots", 6, "number of robots")
	nTasks := flag.Int("tasks", 14, "number of tasks")
	seed := flag.Int64("seed", 42, "random seed for the scenario")
	lossP := flag.Float64("loss", 0.4, "message drop probability during the loss window")
	lossRounds := flag.Int("loss-rounds", 6, "number of initial rounds subject to message loss")
	out := flag.String("out", "report.html", "output HTML file")
	flag.Parse()

	rng := rand.New(rand.NewSource(*seed))
	robots := make([]cbba.Robot, *nRobots)
	for i := range robots {
		robots[i] = cbba.Robot{
			ID:        i,
			X:         rng.Float64() * 100,
			Y:         rng.Float64() * 100,
			Capacity:  4,
			MaxTravel: 400,
		}
	}
	tasks := make([]cbba.Task, *nTasks)
	for j := range tasks {
		tasks[j] = cbba.Task{
			ID:     j,
			X:      rng.Float64() * 100,
			Y:      rng.Float64() * 100,
			Reward: 50 + rng.Float64()*50,
		}
	}
	// One deliberately unreachable task: tiny reward, far outside every
	// robot's travel budget. It must be reported, not silently dropped.
	tasks = append(tasks, cbba.Task{ID: len(tasks), X: 10000, Y: 10000, Reward: 10})

	cfg := cbba.Config{
		Robots: robots,
		Tasks:  tasks,
		Lambda: 1,
		Loss:   cbba.RandomLoss(*seed+1, *lossP, *lossRounds),
	}
	res := cbba.Run(cfg)

	fmt.Printf("收敛: %v（%d 轮）\n", res.Converged, res.Rounds)
	total := 0
	for rid, bundle := range res.Assignment {
		fmt.Printf("  R%-2d -> %v\n", rid, bundle)
		total += len(bundle)
	}
	fmt.Printf("已分配 %d 个任务；无法承接 %v；可承接但未分配 %v；冲突 %d 起\n",
		total, res.Unassignable, res.Unassigned, len(res.Conflicts))

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建报告文件失败:", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := cbba.WriteReport(f, cfg, res); err != nil {
		fmt.Fprintln(os.Stderr, "生成报告失败:", err)
		os.Exit(1)
	}
	fmt.Println("报告已写入", *out)
}
