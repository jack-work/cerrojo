package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/jack-work/cerrojo/internal/ipc"
	"github.com/jack-work/cerrojo/internal/logind"
	"github.com/jack-work/cerrojo/internal/step"
	"github.com/jack-work/cerrojo/internal/wayland"
)

type Config struct {
	Steps           []step.Step
	Grace           time.Duration
	LockCommand     string
	LockBeforeSleep bool
}

type request struct {
	cmd   string
	arg   string
	reply chan ipc.Reply
	watch chan ipc.State
}

type daemon struct {
	cfg       Config
	core      core
	statePath string
	reqs      chan request
	subs      map[chan ipc.State]struct{}
	published ipc.State
}

func Run(ctx context.Context, cfg Config) error {
	if len(cfg.Steps) == 0 {
		return errors.New("no steps")
	}
	sockPath, err := ipc.SocketPath()
	if err != nil {
		return err
	}
	statePath, err := ipc.StatePath()
	if err != nil {
		return err
	}
	ln, err := listen(sockPath)
	if err != nil {
		return err
	}
	defer os.Remove(sockPath)
	defer ln.Close()

	wl, err := wayland.Dial()
	if err != nil {
		return fmt.Errorf("wayland: %w", err)
	}
	defer wl.Close()

	d := newDaemon(cfg, statePath)
	events := make(chan wayland.Event, 8)
	wlErr := make(chan error, 1)
	go func() { wlErr <- wayland.Watch(wl, cfg.Grace, func(e wayland.Event) { events <- e }) }()
	go d.serve(ln)

	sleeps := make(chan chan struct{})
	if cfg.LockBeforeSleep {
		go func() {
			err := logind.LockBeforeSleep(ctx, func() <-chan struct{} {
				done := make(chan struct{})
				select {
				case sleeps <- done:
				case <-ctx.Done():
					close(done)
				}
				return done
			})
			if err != nil {
				log.Printf("no lock before sleep: %v", err)
			}
		}()
	}

	log.Printf("steps %s, now %s, grace %s", strings.Join(d.core.state().Steps, " "), d.core.current(), cfg.Grace)
	return d.loop(ctx, events, wlErr, sleeps)
}

func newDaemon(cfg Config, statePath string) *daemon {
	d := &daemon{
		cfg:       cfg,
		core:      core{steps: cfg.Steps, grace: cfg.Grace},
		statePath: statePath,
		reqs:      make(chan request),
		subs:      map[chan ipc.State]struct{}{},
	}
	if b, err := os.ReadFile(statePath); err == nil {
		if s, err := step.Parse(strings.TrimSpace(string(b))); err == nil {
			d.core.set(s)
		}
	}
	d.published = d.core.state()
	return d
}

func (d *daemon) loop(ctx context.Context, events <-chan wayland.Event, wlErr <-chan error, sleeps <-chan chan struct{}) error {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-wlErr:
			return err
		case e := <-events:
			if e == wayland.Idled {
				d.core.idled(time.Now())
			} else {
				d.core.resumed()
			}
		case r := <-d.reqs:
			d.handle(r)
		case <-timer.C:
			if at, ok := d.core.deadline(); ok && !time.Now().Before(at) {
				d.lock("idle for " + d.core.current().String())
			}
		case done := <-sleeps:
			if d.core.locked {
				close(done)
				break
			}
			locked := d.lock("going to sleep")
			go func() { <-locked; close(done) }()
		}

		if at, ok := d.core.deadline(); ok {
			timer.Reset(max(time.Until(at), 0))
		} else {
			timer.Stop()
		}
		d.publish()
	}
}

func (d *daemon) handle(r request) {
	switch r.cmd {
	case "watch":
		d.subs[r.watch] = struct{}{}
		r.watch <- d.core.state()
		return
	case "unwatch":
		delete(d.subs, r.watch)
		return
	}

	var err error
	switch r.cmd {
	case "status":
	case "next":
		d.core.move(1)
	case "prev":
		d.core.move(-1)
	case "set":
		var s step.Step
		if s, err = step.Parse(r.arg); err == nil {
			err = d.core.set(s)
		}
	default:
		err = fmt.Errorf("unknown command %q", r.cmd)
	}
	if err != nil {
		r.reply <- ipc.Reply{Error: err.Error()}
		return
	}
	if r.cmd != "status" {
		d.persist()
	}
	s := d.core.state()
	r.reply <- ipc.Reply{State: &s}
}

func (d *daemon) persist() {
	if err := os.WriteFile(d.statePath, []byte(d.core.current().String()+"\n"), 0o600); err != nil {
		log.Printf("remembering the step: %v", err)
	}
}

func (d *daemon) publish() {
	s := d.core.state()
	if s.Step == d.published.Step && s.Idle == d.published.Idle && s.IdleSince == d.published.IdleSince &&
		s.Locked == d.published.Locked && slices.Equal(s.Steps, d.published.Steps) {
		return
	}
	d.published = s
	for ch := range d.subs {
		select {
		case <-ch:
		default:
		}
		ch <- s
	}
}

func (d *daemon) lock(reason string) <-chan struct{} {
	d.core.locked = true
	done := make(chan struct{})
	log.Printf("locking: %s", reason)
	cmd := exec.Command("/bin/sh", "-c", d.cfg.LockCommand)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("lock command: %v", err)
		close(done)
		return done
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("lock command: %v", err)
		}
		close(done)
	}()
	return done
}

func listen(path string) (net.Listener, error) {
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return nil, fmt.Errorf("another cerrojo is already listening on %s", path)
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func (d *daemon) serve(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go d.converse(c)
	}
}

func (d *daemon) converse(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return
	}
	cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	enc := json.NewEncoder(c)

	if cmd == "watch" {
		ch := make(chan ipc.State, 1)
		d.reqs <- request{cmd: "watch", watch: ch}
		defer func() { d.reqs <- request{cmd: "unwatch", watch: ch} }()
		gone := make(chan struct{})
		go func() { io.Copy(io.Discard, r); close(gone) }()
		for {
			select {
			case s := <-ch:
				if err := enc.Encode(ipc.Reply{State: &s}); err != nil {
					return
				}
			case <-gone:
				return
			}
		}
	}

	reply := make(chan ipc.Reply, 1)
	d.reqs <- request{cmd: cmd, arg: arg, reply: reply}
	enc.Encode(<-reply)
}
