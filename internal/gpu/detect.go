package gpu

import "context"

// Detect returns every available vendor backend. An empty result means no GPU
// telemetry is reachable, which callers must treat as non-fatal.
func Detect() []Sampler {
	var samplers []Sampler
	if s, ok := NewNVIDIA(); ok {
		samplers = append(samplers, s)
	}
	if s, ok := NewAMD(); ok {
		samplers = append(samplers, s)
	}
	return samplers
}

// Multi fans a sample out across every detected backend.
type Multi struct{ samplers []Sampler }

// NewMulti builds a combined sampler over all detected backends.
func NewMulti() *Multi { return &Multi{samplers: Detect()} }

// Available reports whether any backend was detected.
func (m *Multi) Available() bool { return len(m.samplers) > 0 }

// Sample collects from every backend, ignoring individual backend failures so
// one broken vendor tool cannot hide a working one.
func (m *Multi) Sample(ctx context.Context) []Device {
	var all []Device
	for _, s := range m.samplers {
		devices, err := s.Sample(ctx)
		if err != nil {
			continue
		}
		all = append(all, devices...)
	}
	return all
}
