package logind

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	service = "org.freedesktop.login1"
	path    = "/org/freedesktop/login1"
	manager = "org.freedesktop.login1.Manager"
)

func LockBeforeSleep(ctx context.Context, lock func() <-chan struct{}) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(path),
		dbus.WithMatchInterface(manager),
		dbus.WithMatchMember("PrepareForSleep"),
	); err != nil {
		return err
	}
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)

	obj := conn.Object(service, path)
	inhibit := func() (*os.File, error) {
		var fd dbus.UnixFD
		err := obj.Call(manager+".Inhibit", 0, "sleep", "cerrojo", "Lock the screen before sleep", "delay").Store(&fd)
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(fd), "logind-sleep-inhibitor"), nil
	}

	held, err := inhibit()
	if err != nil {
		return err
	}
	defer func() {
		if held != nil {
			held.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-signals:
			if !ok {
				return errors.New("system bus went away")
			}
			if sig.Name != manager+".PrepareForSleep" || len(sig.Body) != 1 {
				continue
			}
			sleeping, _ := sig.Body[0].(bool)
			if sleeping {
				select {
				case <-lock():
				case <-time.After(10 * time.Second):
					log.Print("lock command still running at sleep; letting the machine go")
				}
				if held != nil {
					held.Close()
					held = nil
				}
				continue
			}
			if held == nil {
				if held, err = inhibit(); err != nil {
					return err
				}
			}
		}
	}
}
