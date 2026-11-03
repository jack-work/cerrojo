package bar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/jack-work/cerrojo/internal/ipc"
)

const (
	closed = "\U000F033E"
	open   = "\U000F033F"
	soon   = 30 * time.Second
)

var wheel = []string{
	"\U000F0A9E", "\U000F0A9F", "\U000F0AA0", "\U000F0AA1",
	"\U000F0AA2", "\U000F0AA3", "\U000F0AA4", "\U000F0AA5",
}

type Output struct {
	Text    string   `json:"text"`
	Tooltip string   `json:"tooltip"`
	Class   []string `json:"class"`
}

func Render(s *ipc.State, now time.Time) Output {
	if s == nil {
		return Output{
			Text:    closed + " ?",
			Tooltip: "cerrojo is not running: nothing will lock the screen",
			Class:   []string{"down"},
		}
	}
	out := Output{Tooltip: Describe(s, now) + "\nclick: next, right-click: previous"}
	switch {
	case s.Timeout == 0:
		out.Text, out.Class = open+" ∞", []string{"never"}
	case s.Locked:
		out.Text, out.Class = closed, []string{"locked"}
	case !s.Idle:
		out.Text, out.Class = closed+" "+s.Step, []string{"active"}
	default:
		idle, left := countdown(s, now)
		slice := int(math.Ceil(float64(idle) / float64(s.TimeoutDuration()) * float64(len(wheel))))
		slice = min(max(slice, 1), len(wheel))
		out.Text, out.Class = closed+" "+wheel[slice-1]+" "+clock(secondsUp(left)), []string{"idle"}
		if left <= soon {
			out.Class = append(out.Class, "soon")
		}
	}
	return out
}

func Describe(s *ipc.State, now time.Time) string {
	var line string
	switch {
	case s.Timeout == 0:
		line = "Never locks on idle"
	case s.Locked:
		line = "Locked"
	case !s.Idle:
		line = "Locks after " + s.Step + " idle"
	default:
		idle, left := countdown(s, now)
		line = fmt.Sprintf("Idle for %s, locks in %s", clock(int(idle/time.Second)), clock(secondsUp(left)))
	}
	parts := make([]string, len(s.Steps))
	for i, st := range s.Steps {
		if st == s.Step {
			st = "[" + st + "]"
		}
		parts[i] = st
	}
	return line + "\n" + strings.Join(parts, " ")
}

func countdown(s *ipc.State, now time.Time) (idle, left time.Duration) {
	idle = max(now.Sub(s.IdleStart()), 0)
	return idle, max(s.TimeoutDuration()-idle, 0)
}

func secondsUp(d time.Duration) int { return int(math.Ceil(d.Seconds())) }

func clock(secs int) string {
	h, m, s := secs/3600, secs%3600/60, secs%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func Stream(ctx context.Context, w io.Writer) error {
	states := make(chan *ipc.State)
	go func() {
		for {
			ipc.Follow(ctx, func(s ipc.State) {
				select {
				case states <- &s:
				case <-ctx.Done():
				}
			})
			if ctx.Err() != nil {
				return
			}
			select {
			case states <- nil:
			case <-ctx.Done():
				return
			}
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}()

	var cur *ipc.State
	last := ""
	emit := func() error {
		b, err := json.Marshal(Render(cur, time.Now()))
		if err != nil {
			return err
		}
		if string(b) == last {
			return nil
		}
		last = string(b)
		_, err = w.Write(append(b, '\n'))
		return err
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case cur = <-states:
			if err := emit(); err != nil {
				return err
			}
		case <-tick.C:
			if cur != nil && cur.Idle && !cur.Locked {
				if err := emit(); err != nil {
					return err
				}
			}
		}
	}
}
