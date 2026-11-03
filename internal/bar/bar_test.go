package bar

import (
	"strings"
	"testing"
	"time"

	"github.com/jack-work/cerrojo/internal/ipc"
)

var steps = []string{"5m", "30m", "1h", "never"}

func idleFor(d time.Duration, now time.Time) *ipc.State {
	return &ipc.State{Step: "5m", Timeout: 300, Steps: steps, Idle: true, IdleSince: now.Add(-d).UnixMilli()}
}

func TestActiveShowsTheLockAndTheStep(t *testing.T) {
	out := Render(&ipc.State{Step: "30m", Timeout: 1800, Steps: steps}, time.Now())
	if out.Text != closed+" 30m" || out.Class[0] != "active" {
		t.Fatalf("%+v", out)
	}
	if !strings.Contains(out.Tooltip, "5m [30m] 1h never") {
		t.Fatalf("tooltip %q does not mark the current step", out.Tooltip)
	}
}

func TestTheWheelFillsAsTheLockNears(t *testing.T) {
	now := time.UnixMilli(time.Now().UnixMilli())
	for _, c := range []struct {
		idle  time.Duration
		slice int
		left  string
		soon  bool
	}{
		{10 * time.Second, 1, "4:50", false},
		{150 * time.Second, 4, "2:30", false},
		{151 * time.Second, 5, "2:29", false},
		{280 * time.Second, 8, "0:20", true},
		{400 * time.Second, 8, "0:00", true},
	} {
		out := Render(idleFor(c.idle, now), now)
		want := closed + " " + wheel[c.slice-1] + " " + c.left
		if out.Text != want {
			t.Errorf("idle %s: %q, want %q", c.idle, out.Text, want)
		}
		if got := len(out.Class) == 2 && out.Class[1] == "soon"; got != c.soon {
			t.Errorf("idle %s: class %v", c.idle, out.Class)
		}
	}
}

func TestNeverShowsAnOpenLock(t *testing.T) {
	out := Render(&ipc.State{Step: "never", Steps: steps, Idle: true, IdleSince: time.Now().UnixMilli()}, time.Now())
	if out.Text != open+" ∞" || out.Class[0] != "never" {
		t.Fatalf("%+v", out)
	}
}

func TestElapsedRoundsDownWhileRemainingRoundsUp(t *testing.T) {
	now := time.UnixMilli(time.Now().UnixMilli())
	out := Render(idleFor(1500*time.Millisecond, now), now)
	if !strings.HasPrefix(out.Tooltip, "Idle for 0:01, locks in 4:59") {
		t.Fatalf("tooltip %q", out.Tooltip)
	}
}

func TestADeadDaemonIsVisible(t *testing.T) {
	if out := Render(nil, time.Now()); out.Class[0] != "down" || !strings.Contains(out.Tooltip, "nothing will lock") {
		t.Fatalf("%+v", out)
	}
}

func TestClockGrowsAnHourField(t *testing.T) {
	for d, want := range map[time.Duration]string{
		59*time.Minute + 50*time.Second: "59:50",
		time.Hour + 5*time.Second:       "1:00:05",
		1500 * time.Millisecond:         "0:02",
	} {
		if got := clock(secondsUp(d)); got != want {
			t.Errorf("clock(%s) = %s, want %s", d, got, want)
		}
	}
}
