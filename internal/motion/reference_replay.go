package motion

import "time"

// RefFrame retains only UI timing, independently of document/geometry resources.
// Native uses a fixed ring to replay dormant scalar state without inventing a
// catch-up timestep or losing the frame that consumes a delayed target.
type RefFrame struct {
	Now   time.Time
	Delta time.Duration
}

// ReplayFrames is equivalent to calling AdvanceFrame for each frame in order.
// It deliberately preserves intermediate arrival snaps and queue subtraction;
// replacing this with one large analytical step changes reference edge cases.
func (spring *RefSpring) ReplayFrames(frames []RefFrame) {
	for _, frame := range frames {
		spring.AdvanceFrame(frame.Now, frame.Delta)
	}
}

// ReplayOutside commits frames only if every rendered frame remains strictly
// outside the closed [low,high] interval. It samples committed positions, never
// a future wall-clock preview. On false the receiver is unchanged. This is an
// exact visibility proof, allowing Native to omit detached DOM-style work while
// retaining all the reference spring's queue/arrival decisions.
func (spring *RefSpring) ReplayOutside(frames []RefFrame, low, high float64) bool {
	if low > high || !finite(low) || !finite(high) {
		return false
	}
	candidate := *spring
	if candidate.position >= low && candidate.position <= high {
		return false
	}
	for _, frame := range frames {
		candidate.AdvanceFrame(frame.Now, frame.Delta)
		if candidate.position >= low && candidate.position <= high {
			return false
		}
	}
	*spring = candidate
	return true
}
