package cbba

import (
	"math/rand"
	"testing"
)

// helper: count how many robots own task j in the final assignment.
func ownersOf(res *Result, j int) []int {
	var out []int
	for rid, bundle := range res.Assignment {
		for _, id := range bundle {
			if id == j {
				out = append(out, rid)
			}
		}
	}
	return out
}

// 竞价冲突仲裁：出价更高者赢得任务。
func TestHigherBidWins(t *testing.T) {
	cfg := Config{
		Robots: []Robot{
			{ID: 0, X: 0, Y: 0, Capacity: 2},
			{ID: 1, X: 9, Y: 0, Capacity: 2},
		},
		Tasks:  []Task{{ID: 0, X: 1, Y: 0, Reward: 100}},
		Lambda: 1,
	}
	res := Run(cfg)
	if !res.Converged {
		t.Fatal("未收敛")
	}
	owners := ownersOf(res, 0)
	if len(owners) != 1 || owners[0] != 0 {
		t.Fatalf("任务应由出价更高的 R0 获得，实际归属 %v", owners)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("存在冲突: %+v", res.Conflicts)
	}
}

// 竞价冲突仲裁：出价相同（平局）时按机器人 ID 仲裁，ID 小者胜。
func TestTieBreakByRobotID(t *testing.T) {
	cfg := Config{
		Robots: []Robot{
			{ID: 0, X: 0, Y: 0, Capacity: 2},
			{ID: 1, X: 10, Y: 0, Capacity: 2},
		},
		Tasks:  []Task{{ID: 0, X: 5, Y: 0, Reward: 100}}, // 双方出价均为 95
		Lambda: 1,
	}
	res := Run(cfg)
	if !res.Converged {
		t.Fatal("未收敛")
	}
	owners := ownersOf(res, 0)
	if len(owners) != 1 || owners[0] != 0 {
		t.Fatalf("平局应由 ID 较小的 R0 获得，实际归属 %v", owners)
	}
}

// 通信丢包：前若干轮全部消息丢失，之后信道恢复，必须仍收敛到无冲突分配。
func TestConvergesAfterBlackout(t *testing.T) {
	mkCfg := func(loss LossFunc) Config {
		return Config{
			Robots: []Robot{
				{ID: 0, X: 0, Y: 0, Capacity: 3},
				{ID: 1, X: 10, Y: 0, Capacity: 3},
				{ID: 2, X: 5, Y: 8, Capacity: 3},
			},
			Tasks: []Task{
				{ID: 0, X: 4, Y: 1, Reward: 100},
				{ID: 1, X: 6, Y: 1, Reward: 100},
				{ID: 2, X: 5, Y: 3, Reward: 100},
				{ID: 3, X: 1, Y: 2, Reward: 100},
			},
			Lambda: 1,
			Loss:   loss,
		}
	}
	clean := Run(mkCfg(nil))
	lossy := Run(mkCfg(BlackoutLoss(4)))

	if !lossy.Converged {
		t.Fatal("丢包后未收敛")
	}
	if len(lossy.Conflicts) != 0 {
		t.Fatalf("丢包场景最终仍有冲突: %+v", lossy.Conflicts)
	}
	if len(lossy.Unassigned) != 0 {
		t.Fatalf("丢包场景有任务被遗漏: %v", lossy.Unassigned)
	}
	if lossy.Rounds <= clean.Rounds {
		t.Fatalf("丢包应需要更多轮次（丢包 %d 轮 vs 无丢包 %d 轮）", lossy.Rounds, clean.Rounds)
	}
	// 丢包窗口内应当确实出现过冲突事件（各方独立竞价、互不知情）。
	sawConflict, sawDrop := false, false
	for _, rl := range lossy.Log {
		for _, ev := range rl.Events {
			if ev.Type == EvConflict {
				sawConflict = true
			}
			if ev.Type == EvDrop {
				sawDrop = true
			}
		}
	}
	if !sawConflict || !sawDrop {
		t.Fatalf("日志中应同时出现丢包与冲突事件 (drop=%v, conflict=%v)", sawDrop, sawConflict)
	}
	// 丢包场景的最终分配必须与无丢包场景一致（仲裁规则是确定性的）。
	for j := 0; j < 4; j++ {
		a, b := ownersOf(clean, j), ownersOf(lossy, j)
		if len(a) != 1 || len(b) != 1 || a[0] != b[0] {
			t.Fatalf("任务 %d 归属不一致: 无丢包 %v, 丢包 %v", j, a, b)
		}
	}
}

// 随机丢包（非全丢）下也必须收敛且无冲突。
func TestConvergesUnderRandomLoss(t *testing.T) {
	cfg := Config{
		Robots: []Robot{
			{ID: 0, X: 0, Y: 0, Capacity: 4},
			{ID: 1, X: 20, Y: 0, Capacity: 4},
			{ID: 2, X: 10, Y: 20, Capacity: 4},
			{ID: 3, X: 30, Y: 20, Capacity: 4},
		},
		Lambda: 1,
		Loss:   RandomLoss(7, 0.5, 10),
	}
	for j := 0; j < 10; j++ {
		cfg.Tasks = append(cfg.Tasks, Task{
			ID: j, X: float64(j * 3), Y: float64((j * 7) % 23), Reward: 80,
		})
	}
	res := Run(cfg)
	if !res.Converged {
		t.Fatal("随机丢包下未收敛")
	}
	rep := Validate(cfg.Robots, cfg.Tasks, 1, res.Assignment)
	if len(rep.Conflicts) != 0 {
		t.Fatalf("最终分配存在冲突: %+v", rep.Conflicts)
	}
}

// 无法承接的任务必须被明确报告，而不是静默丢弃。
func TestUnassignableTaskReported(t *testing.T) {
	cfg := Config{
		Robots: []Robot{
			{ID: 0, X: 0, Y: 0, Capacity: 4, MaxTravel: 50},
			{ID: 1, X: 10, Y: 0, Capacity: 4, MaxTravel: 50},
		},
		Tasks: []Task{
			{ID: 0, X: 3, Y: 0, Reward: 100},
			{ID: 1, X: 1000, Y: 1000, Reward: 10}, // 超出所有机器人的行程预算
			{ID: 2, X: 20, Y: 0, Reward: 1},       // 收益不足以覆盖成本
		},
		Lambda: 1,
	}
	res := Run(cfg)
	if !res.Converged {
		t.Fatal("未收敛")
	}
	if len(res.Unassignable) != 2 {
		t.Fatalf("应报告 2 个无法承接任务，实际 %v", res.Unassignable)
	}
	for _, j := range res.Unassignable {
		if owners := ownersOf(res, j); len(owners) != 0 {
			t.Fatalf("无法承接任务 %d 不应出现在任何任务包中（R%v）", j, owners)
		}
	}
	if got := ownersOf(res, 0); len(got) != 1 {
		t.Fatalf("正常任务 0 应被分配，实际归属 %v", got)
	}
}

// 大规模场景：分配结果必须满足无冲突约束，且每个任务都有明确去向。
func TestLargeScaleConflictFree(t *testing.T) {
	rng := rand.New(rand.NewSource(2026))
	const nRobots, nTasks = 40, 150
	cfg := Config{Lambda: 1, Loss: RandomLoss(99, 0.3, 8)}
	for i := 0; i < nRobots; i++ {
		cfg.Robots = append(cfg.Robots, Robot{
			ID: i, X: rng.Float64() * 200, Y: rng.Float64() * 200,
			Capacity: 6, MaxTravel: 800,
		})
	}
	for j := 0; j < nTasks; j++ {
		cfg.Tasks = append(cfg.Tasks, Task{
			ID: j, X: rng.Float64() * 200, Y: rng.Float64() * 200,
			Reward: 50 + rng.Float64()*50,
		})
	}
	res := Run(cfg)
	if !res.Converged {
		t.Fatalf("大规模场景未收敛（%d 轮）", res.Rounds)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("大规模场景存在 %d 起冲突: %+v", len(res.Conflicts), res.Conflicts[:1])
	}
	// 每个任务恰好属于以下三类之一：已分配 / 无法承接 / 可承接但未分配。
	seen := map[int]string{}
	for _, bundle := range res.Assignment {
		for _, j := range bundle {
			if _, dup := seen[j]; dup {
				t.Fatalf("任务 %d 被分配多次", j)
			}
			seen[j] = "assigned"
		}
	}
	for _, j := range res.Unassignable {
		seen[j] = "unassignable"
	}
	for _, j := range res.Unassigned {
		seen[j] = "unassigned"
	}
	for _, task := range cfg.Tasks {
		if _, ok := seen[task.ID]; !ok {
			t.Fatalf("任务 %d 没有任何去向记录（被静默丢弃）", task.ID)
		}
	}
	// 容量约束。
	for rid, bundle := range res.Assignment {
		if len(bundle) > cfg.Robots[rid].Capacity {
			t.Fatalf("R%d 超出容量: %d > %d", rid, len(bundle), cfg.Robots[rid].Capacity)
		}
	}
	t.Logf("收敛于 %d 轮；分配 %d，无法承接 %d，未分配 %d",
		res.Rounds, nTasks-len(res.Unassignable)-len(res.Unassigned),
		len(res.Unassignable), len(res.Unassigned))
}

// 协作任务允许多个机器人共同承接，不计为冲突。
func TestCooperativeTaskExemptFromConflicts(t *testing.T) {
	cfg := Config{
		Robots: []Robot{
			{ID: 0, X: 0, Y: 0, Capacity: 3},
			{ID: 1, X: 4, Y: 0, Capacity: 3},
		},
		Tasks:  []Task{{ID: 0, X: 2, Y: 0, Reward: 100, Coop: true}},
		Lambda: 1,
	}
	res := Run(cfg)
	if !res.Converged {
		t.Fatal("未收敛")
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("协作任务不应产生冲突: %+v", res.Conflicts)
	}
	if got := len(ownersOf(res, 0)); got != 2 {
		t.Fatalf("协作任务应被两个机器人共同承接，实际 %d 个", got)
	}
}

// Validate 本身必须能检出人为构造的冲突分配。
func TestValidateDetectsConflict(t *testing.T) {
	robots := []Robot{{ID: 0, Capacity: 2}, {ID: 1, Capacity: 2}}
	tasks := []Task{{ID: 0, X: 1, Reward: 10}}
	rep := Validate(robots, tasks, 1, map[int][]int{0: {0}, 1: {0}})
	if rep.OK || len(rep.Conflicts) != 1 {
		t.Fatalf("应检出 1 起冲突，实际 %+v", rep)
	}
}
