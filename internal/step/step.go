package step

import (
	"fmt"
	"strings"
	"time"
)

type Step time.Duration

const Never Step = 0

func Parse(s string) (Step, error) {
	switch strings.TrimSpace(s) {
	case "never", "off":
		return Never, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("step %q: want a duration like 5m or 1h, or never", s)
	}
	if d < time.Second {
		return 0, fmt.Errorf("step %q: shorter than a second", s)
	}
	return Step(d), nil
}

func ParseList(s string) ([]Step, error) {
	var steps []Step
	for _, f := range strings.Split(s, ",") {
		st, err := Parse(f)
		if err != nil {
			return nil, err
		}
		steps = append(steps, st)
	}
	return steps, nil
}

func (s Step) Duration() time.Duration { return time.Duration(s) }

func (s Step) String() string {
	if s == Never {
		return "never"
	}
	d := time.Duration(s).Round(time.Second)
	h, m, sec := int(d/time.Hour), int(d%time.Hour/time.Minute), int(d%time.Minute/time.Second)
	var b strings.Builder
	if h > 0 {
		fmt.Fprintf(&b, "%dh", h)
	}
	if m > 0 {
		fmt.Fprintf(&b, "%dm", m)
	}
	if sec > 0 {
		fmt.Fprintf(&b, "%ds", sec)
	}
	return b.String()
}
