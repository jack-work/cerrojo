package step

import (
	"testing"
	"time"
)

func TestStepsRoundTrip(t *testing.T) {
	for _, s := range []string{"5m", "30m", "1h", "1h30m", "45s", "never"} {
		st, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		if st.String() != s {
			t.Errorf("Parse(%q).String() = %q", s, st.String())
		}
	}
}

func TestParseListKeepsOrder(t *testing.T) {
	got, err := ParseList("5m, 30m,1h,never")
	if err != nil {
		t.Fatal(err)
	}
	want := []Step{Step(5 * time.Minute), Step(30 * time.Minute), Step(time.Hour), Never}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestParseRefusesNonsense(t *testing.T) {
	for _, s := range []string{"", "soon", "10ms", "-5m"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) accepted", s)
		}
	}
}

func TestParseListRefusesARepeatedStep(t *testing.T) {
	if _, err := ParseList("5m,1h,300s"); err == nil {
		t.Fatal("5m and 300s are the same step, and cycling would visit it twice")
	}
}
