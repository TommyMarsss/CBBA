// Package cbba implements the Consensus-Based Bundle Algorithm (CBBA)
// for decentralized multi-robot task allocation, together with a
// synchronous-round simulator that supports message loss, convergence
// detection and conflict/validity reporting.
//
// The algorithm alternates two phases (Choi, Brunet & How, 2009):
//
//  1. Bundle construction: each robot greedily inserts the task with the
//     highest positive marginal score into its bundle.
//  2. Consensus: robots broadcast their winning-bid / winner / timestamp
//     vectors and reconcile conflicts with deterministic rules
//     (higher bid wins; ties broken by lower robot ID).
//
// Only the Go standard library is used.
package cbba

import "math"

// Task is a unit of work located at (X, Y). Reward is earned when the
// task is executed. Coop tasks may be executed by several robots and are
// exempt from the single-winner conflict check.
type Task struct {
	ID     int
	X, Y   float64
	Reward float64
	Coop   bool
}

// Robot is an auction participant. Capacity bounds the bundle size;
// MaxTravel (when > 0) bounds the total path length the robot may fly.
// A task whose best insertion would exceed MaxTravel cannot be accepted
// by this robot.
type Robot struct {
	ID        int
	X, Y      float64
	Capacity  int
	MaxTravel float64
}

// noWinner is the sentinel winner ID for "task currently unassigned".
const noWinner = -1

// eps is the tolerance used when comparing bids for equality (tie-break).
const eps = 1e-9

// dist is the Euclidean distance between two points.
func dist(x1, y1, x2, y2 float64) float64 {
	return math.Hypot(x1-x2, y1-y2)
}

// wins reports whether a challenger (bid, id) beats the incumbent
// (curBid, curWinner). Higher bid wins; exact ties go to the lower robot
// ID, which makes arbitration total, deterministic and identical on
// every robot that sees the same pair of bids.
func wins(bid float64, id int, curBid float64, curWinner int) bool {
	if curWinner == noWinner {
		return bid > 0
	}
	if bid > curBid+eps {
		return true
	}
	if math.Abs(bid-curBid) <= eps {
		return id < curWinner
	}
	return false
}
