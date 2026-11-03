package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

type State struct {
	Step      string   `json:"step"`
	Timeout   int64    `json:"timeout"`
	Steps     []string `json:"steps"`
	Idle      bool     `json:"idle"`
	IdleSince int64    `json:"idle_since,omitempty"`
	Locked    bool     `json:"locked"`
}

type Reply struct {
	*State
	Error string `json:"error,omitempty"`
}

func (s State) TimeoutDuration() time.Duration { return time.Duration(s.Timeout) * time.Second }

func (s State) IdleStart() time.Time { return time.UnixMilli(s.IdleSince) }

func runtimeDir() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set")
	}
	return dir, nil
}

func SocketPath() (string, error) {
	dir, err := runtimeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cerrojo.sock"), nil
}

func StatePath() (string, error) {
	dir, err := runtimeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cerrojo.step"), nil
}

func dial() (net.Conn, error) {
	path, err := SocketPath()
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("cerrojo is not running: %w", err)
	}
	return conn, nil
}

func Ask(cmd string) (State, error) {
	conn, err := dial()
	if err != nil {
		return State{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintln(conn, cmd); err != nil {
		return State{}, err
	}
	sc := bufio.NewScanner(conn)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return State{}, err
		}
		return State{}, errors.New("cerrojo hung up without answering")
	}
	return decode(sc.Bytes())
}

func Follow(ctx context.Context, fn func(State)) error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if _, err := fmt.Fprintln(conn, "watch"); err != nil {
		return err
	}
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		s, err := decode(sc.Bytes())
		if err != nil {
			return err
		}
		fn(s)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("cerrojo hung up")
}

func decode(line []byte) (State, error) {
	var r Reply
	if err := json.Unmarshal(line, &r); err != nil {
		return State{}, fmt.Errorf("unreadable answer from cerrojo: %w", err)
	}
	if r.Error != "" {
		return State{}, errors.New(r.Error)
	}
	if r.State == nil {
		return State{}, errors.New("empty answer from cerrojo")
	}
	return *r.State, nil
}
