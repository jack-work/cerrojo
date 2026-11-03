package wayland

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

type fakeCompositor struct {
	t *testing.T
	w *wire
}

func (f *fakeCompositor) expect(obj uint32, op uint16) []byte {
	f.t.Helper()
	gotObj, gotOp, body, err := f.w.recv()
	if err != nil {
		f.t.Fatalf("compositor read: %v", err)
	}
	if gotObj != obj || gotOp != op {
		f.t.Fatalf("request on object %d opcode %d, want object %d opcode %d", gotObj, gotOp, obj, op)
	}
	return body
}

func (f *fakeCompositor) handshake(globals map[uint32][2]any) {
	f.expect(displayID, 1)
	f.expect(displayID, 0)
	for name, g := range globals {
		f.w.send(registryID, 0, u32(name), str(g[0].(string)), u32(g[1].(uint32)))
	}
	f.w.send(syncCallbackID, 0, u32(1))
}

func pipe(t *testing.T) (client net.Conn, f *fakeCompositor) {
	c, s := net.Pipe()
	t.Cleanup(func() { c.Close(); s.Close() })
	return c, &fakeCompositor{t: t, w: &wire{r: bufio.NewReader(s), w: s}}
}

func TestWatchBindsSeatAndNotifierThenReportsIdleAndResume(t *testing.T) {
	client, f := pipe(t)
	go func() {
		f.handshake(map[uint32][2]any{
			7:  {"wl_compositor", uint32(6)},
			9:  {"wl_seat", uint32(9)},
			12: {"ext_idle_notifier_v1", uint32(2)},
		})

		name, iface, version, _ := parseGlobal(f.expect(registryID, 0))
		if name != 9 || iface != "wl_seat" || version != 1 {
			t.Errorf("seat bind: name %d iface %q version %d", name, iface, version)
		}
		name, iface, _, _ = parseGlobal(f.expect(registryID, 0))
		if name != 12 || iface != "ext_idle_notifier_v1" {
			t.Errorf("notifier bind: name %d iface %q", name, iface)
		}
		body := f.expect(notifierID, 1)
		if id, ms, seat := ne.Uint32(body), ne.Uint32(body[4:]), ne.Uint32(body[8:]); id != notificationID || ms != 10000 || seat != seatID {
			t.Errorf("get_idle_notification(%d, %d, %d)", id, ms, seat)
		}

		f.w.send(seatID, 0, u32(3))
		f.w.send(notificationID, 0)
		f.w.send(notificationID, 1)
		f.w.w.(net.Conn).Close()
	}()

	var got []Event
	err := Watch(client, 10*time.Second, func(e Event) { got = append(got, e) })
	if err == nil {
		t.Fatal("Watch returned nil after the compositor hung up")
	}
	if len(got) != 2 || got[0] != Idled || got[1] != Resumed {
		t.Fatalf("events %v, want [Idled Resumed]", got)
	}
}

func TestWatchRefusesACompositorWithoutIdleNotify(t *testing.T) {
	client, f := pipe(t)
	go f.handshake(map[uint32][2]any{9: {"wl_seat", uint32(9)}})

	err := Watch(client, time.Second, func(Event) {})
	if err == nil || !strings.Contains(err.Error(), "ext_idle_notifier_v1") {
		t.Fatalf("err = %v, want one naming ext_idle_notifier_v1", err)
	}
}

func TestWatchSurfacesProtocolErrors(t *testing.T) {
	client, f := pipe(t)
	go func() {
		f.expect(displayID, 1)
		f.expect(displayID, 0)
		f.w.send(displayID, 0, u32(5), u32(2), str("invalid seat"))
	}()

	err := Watch(client, time.Second, func(Event) {})
	if err == nil || !strings.Contains(err.Error(), "invalid seat") {
		t.Fatalf("err = %v, want the compositor's message", err)
	}
}

func TestStringsArePaddedToFourBytes(t *testing.T) {
	for _, s := range []string{"", "abc", "abcd", "wl_seat"} {
		b := str(s)
		if len(b)%4 != 0 {
			t.Errorf("str(%q) is %d bytes", s, len(b))
		}
		got, rest, err := readStr(b)
		if err != nil || got != s || len(rest) != 0 {
			t.Errorf("readStr(str(%q)) = %q, %d left, %v", s, got, len(rest), err)
		}
	}
}
