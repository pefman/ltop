package llama

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/pefman/ltop/internal/promparse"
)

// Expected values are computed from a scrape captured off a live llama.cpp
// server running qwen3.8-27b with speculative decoding enabled.
func TestParseMetricsDerivations(t *testing.T) {
	f, err := os.Open("../promparse/testdata/metrics.txt")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	set, err := promparse.Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m := ParseMetrics(set)

	if !m.SpecEnabled() {
		t.Fatal("SpecEnabled = false, want true")
	}

	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"CacheHitRate", m.CacheHitRate(), 615171.0 / (615171.0 + 201641.0)},
		{"SpecAcceptanceRate", m.SpecAcceptanceRate(), 5542.0 / 9405.0},
		{"SpecTokensPerDraft", m.SpecTokensPerDraft(), 5542.0 / 3139.0},
		{"SpecSpeedup", m.SpecSpeedup(), 5542.0/3139.0 + 1},
		{"LifetimePrefill", m.LifetimePromptTokensPerSec(), 201641.0 / 160.826},
		{"LifetimeDecode", m.LifetimePredictedTokensPerSec(), 8697.0 / 250.856},
	}
	for _, c := range checks {
		if math.Abs(c.got-c.want) > 1e-9 {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	if got := m.CacheHitRate(); got < 0.75 || got > 0.76 {
		t.Errorf("CacheHitRate = %.4f, want ~0.7531", got)
	}
	if got := m.SpecAcceptanceRate(); got < 0.58 || got > 0.60 {
		t.Errorf("SpecAcceptanceRate = %.4f, want ~0.5892", got)
	}

	wantPos := []float64{2368, 1789, 1385}
	if len(m.SpecAcceptedPerPos) != len(wantPos) {
		t.Fatalf("SpecAcceptedPerPos len = %d, want %d", len(m.SpecAcceptedPerPos), len(wantPos))
	}
	for i, w := range wantPos {
		if m.SpecAcceptedPerPos[i] != w {
			t.Errorf("SpecAcceptedPerPos[%d] = %v, want %v", i, m.SpecAcceptedPerPos[i], w)
		}
	}
}

// A server without a draft model emits no speculative counters and must not
// produce divide-by-zero results.
func TestMetricsWithoutSpeculation(t *testing.T) {
	set, err := promparse.Parse(strings.NewReader("llamacpp:tokens_predicted_total 10\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m := ParseMetrics(set)

	if m.SpecEnabled() {
		t.Error("SpecEnabled = true, want false")
	}
	if got := m.SpecAcceptanceRate(); got != 0 {
		t.Errorf("SpecAcceptanceRate = %v, want 0", got)
	}
	if got := m.SpecSpeedup(); got != 1 {
		t.Errorf("SpecSpeedup = %v, want 1", got)
	}
	if got := m.CacheHitRate(); got != 0 {
		t.Errorf("CacheHitRate = %v, want 0", got)
	}
	if got := m.LifetimePredictedTokensPerSec(); got != 0 {
		t.Errorf("LifetimeDecode = %v, want 0", got)
	}
}

func TestNormalizeBase(t *testing.T) {
	cases := map[string]string{
		"http://localhost:11436/v1":  "http://localhost:11436",
		"http://localhost:11436/v1/": "http://localhost:11436",
		"http://localhost:11436/":    "http://localhost:11436",
		"localhost:11436":            "http://localhost:11436",
		"  http://h:1/v1  ":          "http://h:1",
		"":                           "",
	}
	for in, want := range cases {
		if got := NormalizeBase(in); got != want {
			t.Errorf("NormalizeBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidBase(t *testing.T) {
	valid := []string{"http://localhost:11436/v1", "https://example.com", "localhost:8080"}
	for _, v := range valid {
		if !ValidBase(v) {
			t.Errorf("ValidBase(%q) = false, want true", v)
		}
	}
	invalid := []string{"", "   ", "://nope"}
	for _, v := range invalid {
		if ValidBase(v) {
			t.Errorf("ValidBase(%q) = true, want false", v)
		}
	}
}

func TestSlotContextUsed(t *testing.T) {
	if got := (Slot{PromptTokens: 70016, NCtx: 140032}).ContextUsed(); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("ContextUsed = %v, want 0.5", got)
	}
	if got := (Slot{PromptTokens: 10, NCtx: 0}).ContextUsed(); got != 0 {
		t.Errorf("ContextUsed with zero ctx = %v, want 0", got)
	}
	if got := (Slot{PromptTokens: 200, NCtx: 100}).ContextUsed(); got != 1 {
		t.Errorf("ContextUsed overflow = %v, want clamp to 1", got)
	}
}

func TestPropsModelName(t *testing.T) {
	p := Props{ModelPath: "/home/u/Desktop/qwen3.8/Qwen3.8-27B-UD-Q4_K_XL.gguf"}
	if got := p.ModelName(); got != "Qwen3.8-27B-UD-Q4_K_XL" {
		t.Errorf("ModelName = %q", got)
	}
	if got := (Props{}).ModelName(); got != "" {
		t.Errorf("ModelName empty = %q", got)
	}
}

// A router serving several models must not pair one model's name with
// another's size and quantisation.
func TestMatchModel(t *testing.T) {
	qwen := Model{ID: "qwen3.8-27b", Meta: ModelMeta{Size: 17912397824}}
	llama8b := Model{ID: "llama-3.1-8b", Meta: ModelMeta{Size: 8540770304}}
	props := func(p string) Props { return Props{ModelPath: p} }

	t.Run("single model is always used", func(t *testing.T) {
		got, ok := MatchModel([]Model{llama8b}, props("/m/Anything-Else.gguf"))
		if !ok || got.ID != "llama-3.1-8b" {
			t.Errorf("got %q ok=%v", got.ID, ok)
		}
	})

	t.Run("alias shorter than filename", func(t *testing.T) {
		got, ok := MatchModel([]Model{llama8b, qwen}, props("/m/Qwen3.8-27B-UD-Q4_K_XL.gguf"))
		if !ok || got.ID != "qwen3.8-27b" {
			t.Errorf("got %q ok=%v, want qwen3.8-27b", got.ID, ok)
		}
	})

	t.Run("exact id match", func(t *testing.T) {
		got, ok := MatchModel([]Model{llama8b, qwen}, props("/m/llama-3.1-8b.gguf"))
		if !ok || got.ID != "llama-3.1-8b" {
			t.Errorf("got %q ok=%v", got.ID, ok)
		}
	})

	t.Run("no confident match withholds metadata", func(t *testing.T) {
		got, ok := MatchModel([]Model{llama8b, qwen}, props("/m/Phi-4-Q6_K.gguf"))
		if ok {
			t.Errorf("matched %q, want no match", got.ID)
		}
		if got.Meta.Size != 0 {
			t.Error("returned metadata despite no match")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		if _, ok := MatchModel(nil, props("/m/x.gguf")); ok {
			t.Error("matched against an empty model list")
		}
	})
}

func TestHealthStates(t *testing.T) {
	if !(Health{Status: "ok"}).OK() {
		t.Error("ok not recognised")
	}
	if !(Health{Status: "loading model"}).Loading() {
		t.Error("loading not recognised")
	}
	if (Health{Status: "ok"}).Loading() {
		t.Error("ok reported as loading")
	}
}
