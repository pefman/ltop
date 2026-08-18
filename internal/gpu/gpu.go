// Package gpu samples GPU utilisation on Linux from vendor tooling and sysfs.
package gpu

import "context"

// Device is one GPU's sampled state. Fields a vendor cannot report are left at
// their zero value and flagged by the corresponding Has* field.
type Device struct {
	Index  int
	Name   string
	Vendor string

	UtilPercent float64
	HasUtil     bool

	MemUsedBytes  uint64
	MemTotalBytes uint64
	HasMem        bool

	TempCelsius float64
	HasTemp     bool

	PowerWatts      float64
	PowerLimitWatts float64
	HasPower        bool
}

// MemPercent is VRAM occupancy in 0..1.
func (d Device) MemPercent() float64 {
	if d.MemTotalBytes == 0 {
		return 0
	}
	return float64(d.MemUsedBytes) / float64(d.MemTotalBytes)
}

// Sampler reads a set of GPUs from one vendor backend.
type Sampler interface {
	// Name identifies the backend for display.
	Name() string
	// Sample returns the current state of every device the backend can see.
	Sample(ctx context.Context) ([]Device, error)
}
