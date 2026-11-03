package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jack-work/cerrojo/internal/ipc"
	"github.com/jack-work/cerrojo/internal/step"
	"github.com/jack-work/cerrojo/internal/wayland"
)

type rig struct {
	t      *testing.T
	d      *daemon
	events chan wayland.Event
	sleeps chan chan struct{}
	locks  string
}

func start(t *testing.T, steps ...step.Step) *rig {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	r := &rig{t: t, events: make(chan wayland.Event), sleeps: make(chan chan struct{}), locks: filepath.Join(dir, "locks")}
	statePath, _ := ipc.StatePath()
	r.d = newDaemon(Config{Steps: steps, LockCommand: "echo locked >> " + r.locks}, statePath)

	sock, _ := ipc.SocketPath()
	ln, err := listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); ln.Close() })
	go r.d.serve(ln)
	go r.d.loop(ctx, r.events, make(chan error), r.sleeps)
	return r
}

func (r *rig) follow() <-chan ipc.State {
	ch := make(chan ipc.State, 64)
	ctx, cancel := context.WithCancel(context.Background())
	r.t.Cleanup(cancel)
	go ipc.Follow(ctx, func(s ipc.State) { ch <- s })
	return ch
}

func (r *rig) await(ch <-chan ipc.State, within time.Duration, want func(ipc.State) bool) ipc.State {
	r.t.Helper()
	deadline := time.After(within)
	for {
		select {
		case s := <-ch:
			if want(s) {
				return s
			}
		case <-deadline:
			r.t.Fatalf("no matching state within %s", within)
		}
	}
}

func (r *rig) lockCount() int {
	b, _ := os.ReadFile(r.locks)
	return strings.Count(string(b), "locked")
}

func (r *rig) ask(cmd string) ipc.State {
	r.t.Helper()
	s, err := ipc.Ask(cmd)
	if err != nil {
		r.t.Fatalf("%s: %v", cmd, err)
	}
	return s
}

const short = step.Step(200 * time.Millisecond)

func TestIdlePastTheStepLocksOnceAndInputClearsIt(t *testing.T) {
	r := start(t, short, step.Never)
	states := r.follow()
	r.await(states, time.Second, func(s ipc.State) bool { return !s.Idle })

	r.events <- wayland.Idled
	r.await(states, time.Second, func(s ipc.State) bool { return s.Locked })
	time.Sleep(3 * time.Duration(short))
	if n := r.lockCount(); n != 1 {
		t.Fatalf("lock command ran %d times, want 1", n)
	}

	r.events <- wayland.Resumed
	r.await(states, time.Second, func(s ipc.State) bool { return !s.Locked && !s.Idle })
}

func TestNeverDoesNotLock(t *testing.T) {
	r := start(t, step.Never, short)
	states := r.follow()
	r.events <- wayland.Idled
	r.await(states, time.Second, func(s ipc.State) bool { return s.Idle })
	time.Sleep(3 * time.Duration(short))
	if n := r.lockCount(); n != 0 {
		t.Fatalf("locked %d times in never mode", n)
	}
}

func TestSwitchingStepWhileIdleUsesTheIdleStartAlreadyRecorded(t *testing.T) {
	r := start(t, step.Never, short)
	states := r.follow()
	r.events <- wayland.Idled
	r.await(states, time.Second, func(s ipc.State) bool { return s.Idle })
	time.Sleep(2 * time.Duration(short))

	r.ask("next")
	r.await(states, 100*time.Millisecond, func(s ipc.State) bool { return s.Locked })
}

func TestStepsCycleBothWaysAndSurviveARestart(t *testing.T) {
	five, thirty, hour := step.Step(5*time.Minute), step.Step(30*time.Minute), step.Step(time.Hour)
	r := start(t, five, thirty, hour, step.Never)

	for _, want := range []string{"30m", "1h", "never", "5m"} {
		if got := r.ask("next").Step; got != want {
			t.Fatalf("next = %s, want %s", got, want)
		}
	}
	if got := r.ask("prev").Step; got != "never" {
		t.Fatalf("prev from the first step = %s, want never", got)
	}
	if got := r.ask("set 1h").Step; got != "1h" {
		t.Fatalf("set 1h = %s", got)
	}
	if _, err := ipc.Ask("set 2h"); err == nil || !strings.Contains(err.Error(), "5m, 30m, 1h, never") {
		t.Fatalf("set 2h: err = %v, want the list of steps", err)
	}

	statePath, _ := ipc.StatePath()
	again := newDaemon(Config{Steps: []step.Step{five, thirty, hour, step.Never}}, statePath)
	if got := again.core.current().String(); got != "1h" {
		t.Fatalf("after restart the step is %s, want 1h", got)
	}
}

func TestSleepLocksUnlessAlreadyLocked(t *testing.T) {
	r := start(t, short)
	states := r.follow()

	done := make(chan struct{})
	r.sleeps <- done
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sleep lock never finished")
	}
	r.await(states, time.Second, func(s ipc.State) bool { return s.Locked })

	again := make(chan struct{})
	r.sleeps <- again
	<-again
	if n := r.lockCount(); n != 1 {
		t.Fatalf("lock command ran %d times, want 1", n)
	}
}

func TestCountdownStartsWhenInputStopped(t *testing.T) {
	c := core{steps: []step.Step{step.Step(5 * time.Minute)}, grace: 10 * time.Second}
	now := time.Unix(1000, 0)
	c.idled(now)
	at, ok := c.deadline()
	if !ok || !at.Equal(now.Add(5*time.Minute-10*time.Second)) {
		t.Fatalf("deadline %v, %v", at, ok)
	}
}

func TestASecondDaemonRefusesTheSocket(t *testing.T) {
	start(t, short)
	sock, _ := ipc.SocketPath()
	if _, err := listen(sock); err == nil {
		t.Fatal("second listen succeeded")
	}
}
