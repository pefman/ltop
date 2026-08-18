package promparse

import (
	"math"
	"os"
	"strings"
	"testing"
)

func TestParseFixture(t *testing.T) {
	f, err := os.Open("testdata/metrics.txt")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	set, err := Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tests := []struct {
		name string
		want float64
	}{
		{"llamacpp:prompt_tokens_total", 201641},
		{"llamacpp:prompt_tokens_cached_total", 615171},
		{"llamacpp:tokens_predicted_total", 8697},
		{"llamacpp:tokens_predicted_seconds_total", 250.856},
		{"llamacpp:predicted_tokens_seconds", 35.3447},
		{"llamacpp:requests_deferred", 0},
	}
	for _, tt := range tests {
		got, ok := set.Value(tt.name)
		if !ok {
			t.Errorf("%s: missing", tt.name)
			continue
		}
		if math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("%s = %v, want %v", tt.name, got, tt.want)
		}
	}

	if f := set["llamacpp:prompt_tokens_total"]; f.Type != "counter" {
		t.Errorf("type = %q, want counter", f.Type)
	} else if !strings.Contains(f.Help, "excluding cached") {
		t.Errorf("help not captured: %q", f.Help)
	}
}

// A labeled histogram must not be readable as a scalar, or a per-position
// series could be mistaken for a total.
func TestLabeledSeries(t *testing.T) {
	set, err := Parse(strings.NewReader(mustFixture(t)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	const name = "llamacpp:spec_decode_num_accepted_tokens_per_pos_total"
	samples := set.Samples(name)
	if len(samples) != 3 {
		t.Fatalf("got %d samples, want 3", len(samples))
	}
	want := map[string]float64{"0": 2368, "1": 1789, "2": 1385}
	for _, s := range samples {
		pos := s.Labels["position"]
		if w, ok := want[pos]; !ok {
			t.Errorf("unexpected position %q", pos)
		} else if s.Value != w {
			t.Errorf("position %q = %v, want %v", pos, s.Value, w)
		}
	}
	if _, ok := set.Value(name); ok {
		t.Error("labeled series was readable as a scalar")
	}
}

func TestParseEdgeCases(t *testing.T) {
	doc := `
# TYPE a_gauge gauge
a_gauge 1.5 1699999999000
b_metric{k="v,with,commas",j="x"} 42
c_inf +Inf
malformed_line_without_value
d_neg -3.25
`
	set, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := set.ValueOr("a_gauge", -1); got != 1.5 {
		t.Errorf("timestamped value = %v, want 1.5", got)
	}
	if got := set.ValueOr("d_neg", 0); got != -3.25 {
		t.Errorf("negative value = %v, want -3.25", got)
	}
	if got := set.ValueOr("c_inf", 0); !math.IsInf(got, 1) {
		t.Errorf("c_inf = %v, want +Inf", got)
	}
	if _, ok := set["malformed_line_without_value"]; ok {
		t.Error("malformed line was accepted")
	}

	b := set.Samples("b_metric")
	if len(b) != 1 {
		t.Fatalf("b_metric samples = %d, want 1", len(b))
	}
	if b[0].Labels["k"] != "v,with,commas" {
		t.Errorf("quoted comma label = %q", b[0].Labels["k"])
	}
	if b[0].Labels["j"] != "x" {
		t.Errorf("second label = %q, want x", b[0].Labels["j"])
	}
}

func TestValueOrMissing(t *testing.T) {
	set, err := Parse(strings.NewReader("x 1\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := set.ValueOr("absent", 7); got != 7 {
		t.Errorf("ValueOr(absent) = %v, want 7", got)
	}
}

func mustFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/metrics.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}
