package wayland

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

type Event int

const (
	Idled Event = iota
	Resumed
)

const (
	displayID      = 1
	registryID     = 2
	syncCallbackID = 3
	seatID         = 4
	notifierID     = 5
	notificationID = 6
)

var ne = binary.NativeEndian

func SocketPath() (string, error) {
	name := os.Getenv("WAYLAND_DISPLAY")
	if name == "" {
		name = "wayland-0"
	}
	if filepath.IsAbs(name) {
		return name, nil
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set")
	}
	return filepath.Join(dir, name), nil
}

func Dial() (io.ReadWriteCloser, error) {
	path, err := SocketPath()
	if err != nil {
		return nil, err
	}
	return net.Dial("unix", path)
}

type global struct {
	name    uint32
	version uint32
}

func Watch(conn io.ReadWriter, after time.Duration, emit func(Event)) error {
	w := &wire{r: bufio.NewReader(conn), w: conn}

	if err := w.send(displayID, 1, u32(registryID)); err != nil {
		return err
	}
	if err := w.send(displayID, 0, u32(syncCallbackID)); err != nil {
		return err
	}

	globals := map[string]global{}
	for done := false; !done; {
		obj, op, body, err := w.recv()
		if err != nil {
			return err
		}
		switch {
		case obj == displayID && op == 0:
			return displayError(body)
		case obj == registryID && op == 0:
			name, iface, version, err := parseGlobal(body)
			if err != nil {
				return err
			}
			if _, seen := globals[iface]; !seen {
				globals[iface] = global{name, version}
			}
		case obj == syncCallbackID && op == 0:
			done = true
		}
	}

	seat, ok := globals["wl_seat"]
	if !ok {
		return errors.New("compositor advertises no wl_seat")
	}
	notifier, ok := globals["ext_idle_notifier_v1"]
	if !ok {
		return errors.New("compositor lacks ext_idle_notifier_v1")
	}

	if err := w.send(registryID, 0, bind(seat.name, "wl_seat", 1, seatID)...); err != nil {
		return err
	}
	if err := w.send(registryID, 0, bind(notifier.name, "ext_idle_notifier_v1", 1, notifierID)...); err != nil {
		return err
	}
	ms := after.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	if err := w.send(notifierID, 1, u32(notificationID), u32(uint32(ms)), u32(seatID)); err != nil {
		return err
	}

	for {
		obj, op, body, err := w.recv()
		if err != nil {
			return err
		}
		switch {
		case obj == displayID && op == 0:
			return displayError(body)
		case obj == notificationID && op == 0:
			emit(Idled)
		case obj == notificationID && op == 1:
			emit(Resumed)
		}
	}
}

type wire struct {
	r *bufio.Reader
	w io.Writer
}

func (w *wire) send(obj uint32, op uint16, args ...[]byte) error {
	size := 8
	for _, a := range args {
		size += len(a)
	}
	msg := make([]byte, 8, size)
	ne.PutUint32(msg[0:], obj)
	ne.PutUint32(msg[4:], uint32(size)<<16|uint32(op))
	for _, a := range args {
		msg = append(msg, a...)
	}
	_, err := w.w.Write(msg)
	return err
}

func (w *wire) recv() (obj uint32, op uint16, body []byte, err error) {
	var hdr [8]byte
	if _, err = io.ReadFull(w.r, hdr[:]); err != nil {
		return 0, 0, nil, fmt.Errorf("wayland: %w", err)
	}
	obj = ne.Uint32(hdr[0:])
	word := ne.Uint32(hdr[4:])
	size := int(word >> 16)
	if size < 8 {
		return 0, 0, nil, fmt.Errorf("wayland: message of %d bytes", size)
	}
	body = make([]byte, size-8)
	if _, err = io.ReadFull(w.r, body); err != nil {
		return 0, 0, nil, fmt.Errorf("wayland: %w", err)
	}
	return obj, uint16(word), body, nil
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	ne.PutUint32(b, v)
	return b
}

func str(s string) []byte {
	n := len(s) + 1
	b := make([]byte, 4+(n+3)&^3)
	ne.PutUint32(b, uint32(n))
	copy(b[4:], s)
	return b
}

func bind(name uint32, iface string, version, id uint32) [][]byte {
	return [][]byte{u32(name), str(iface), u32(version), u32(id)}
}

func readStr(b []byte) (string, []byte, error) {
	if len(b) < 4 {
		return "", nil, errors.New("wayland: short string")
	}
	n := int(ne.Uint32(b))
	padded := (n + 3) &^ 3
	if n == 0 || len(b) < 4+padded {
		return "", nil, errors.New("wayland: bad string")
	}
	return string(b[4 : 4+n-1]), b[4+padded:], nil
}

func parseGlobal(b []byte) (uint32, string, uint32, error) {
	if len(b) < 4 {
		return 0, "", 0, errors.New("wayland: short global")
	}
	name := ne.Uint32(b)
	iface, rest, err := readStr(b[4:])
	if err != nil {
		return 0, "", 0, err
	}
	if len(rest) < 4 {
		return 0, "", 0, errors.New("wayland: short global")
	}
	return name, iface, ne.Uint32(rest), nil
}

func displayError(b []byte) error {
	if len(b) < 8 {
		return errors.New("wayland: protocol error")
	}
	msg, _, err := readStr(b[8:])
	if err != nil {
		msg = "?"
	}
	return fmt.Errorf("wayland: protocol error on object %d, code %d: %s", ne.Uint32(b), ne.Uint32(b[4:]), msg)
}
