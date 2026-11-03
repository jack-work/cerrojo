package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/jack-work/cerrojo/internal/ipc"
	"github.com/jack-work/cerrojo/internal/step"
)

type core struct {
	steps     []step.Step
	i         int
	grace     time.Duration
	idle      bool
	idleSince time.Time
	locked    bool
}

func (c *core) current() step.Step { return c.steps[c.i] }

func (c *core) idled(now time.Time) {
	c.idle = true
	c.idleSince = now.Add(-c.grace)
}

func (c *core) resumed() {
	c.idle = false
	c.idleSince = time.Time{}
	c.locked = false
}

func (c *core) move(by int) {
	n := len(c.steps)
	c.i = ((c.i+by)%n + n) % n
}

func (c *core) set(s step.Step) error {
	for i, have := range c.steps {
		if have == s {
			c.i = i
			return nil
		}
	}
	names := make([]string, len(c.steps))
	for i, s := range c.steps {
		names[i] = s.String()
	}
	return fmt.Errorf("%s is not one of the steps: %s", s, strings.Join(names, ", "))
}

func (c *core) deadline() (time.Time, bool) {
	if !c.idle || c.locked || c.current() == step.Never {
		return time.Time{}, false
	}
	return c.idleSince.Add(c.current().Duration()), true
}

func (c *core) state() ipc.State {
	s := ipc.State{
		Step:    c.current().String(),
		Timeout: int64(c.current().Duration() / time.Second),
		Idle:    c.idle,
		Locked:  c.locked,
	}
	for _, st := range c.steps {
		s.Steps = append(s.Steps, st.String())
	}
	if c.idle {
		s.IdleSince = c.idleSince.UnixMilli()
	}
	return s
}
