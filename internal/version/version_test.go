package version

import "testing"

func TestSemverCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.3.0", "1.2.9", 1},
		{"v2.8.3", "2.8.3", 0},
		{"2.10.0", "2.9.9", 1},
		{"1.0.0", "1.0.0-rc1", 1},
	}
	for _, c := range cases {
		got := ParseSemver(c.a).Compare(ParseSemver(c.b))
		if got != c.want {
			t.Errorf("Compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestAtLeast(t *testing.T) {
	if !AtLeast("2.8.7", "2.8.7") {
		t.Error("equal should satisfy minimum")
	}
	if !AtLeast("2.9.0", "2.8.7") {
		t.Error("newer should satisfy minimum")
	}
	if AtLeast("2.8.6", "2.8.7") {
		t.Error("older should not satisfy minimum")
	}
}
