package service

import (
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

// Route-aware ceilings. A full draw or mixed timeline needs more writes than a
// short transcript edit; the caps stay finite so a runaway Agent still stops.
const (
	skillStepCap   = 192
	skillSecondCap = 7200
	skillFrameCap  = 128
)

// defaultSkillBudget applies when the caller leaves a field at zero.
// draw is the drawing strategy. visual_heavy and custom without that strategy
// are the mixed / frame-led routes. lightweight stays the editorial default.
func defaultSkillBudget(strategy, flow string) skill.Budget {
	switch {
	case strategy == "draw":
		return skill.Budget{MaxSteps: 128, MaxSeconds: 3600, MaxFrames: 96}
	case flow == "visual_heavy" || flow == "custom":
		return skill.Budget{MaxSteps: 96, MaxSeconds: 2400, MaxFrames: 64}
	default:
		return skill.Budget{MaxSteps: 48, MaxSeconds: 1200, MaxFrames: 36}
	}
}

// skillStepWeight is zero for read-only tools. Listing, getting, reading and
// resolving do not spend a step. Time and frame budgets still apply, and every
// other tool costs one step even when it fails.
func skillStepWeight(name string) int {
	action := name
	if i := strings.LastIndex(name, "."); i >= 0 {
		action = name[i+1:]
	}
	switch action {
	case "list", "get", "read", "get_frames", "get_frame", "capabilities", "steps", "resolve":
		return 0
	default:
		return 1
	}
}

func fillBudgetRemaining(r *skill.Run) {
	if r == nil {
		return
	}
	steps := r.Budget.MaxSteps - (r.Steps - r.BudgetStepBase)
	if steps < 0 {
		steps = 0
	}
	elapsed := r.ElapsedMilliseconds - r.BudgetElapsedBase
	if elapsed < 0 {
		elapsed = 0
	}
	seconds := r.Budget.MaxSeconds - int(elapsed/1000)
	if seconds < 0 {
		seconds = 0
	}
	frames := r.Budget.MaxFrames - r.FramesSpent
	if frames < 0 {
		frames = 0
	}
	r.Remaining = skill.BudgetRemaining{Steps: steps, Seconds: seconds, Frames: frames}
}
