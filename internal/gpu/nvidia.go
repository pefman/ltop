package gpu

import (
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// nvidiaQuery lists the fields requested from nvidia-smi, in output order.
var nvidiaQuery = []string{
	"index",
	"name",
	"utilization.gpu",
	"memory.used",
	"memory.total",
	"temperature.gpu",
	"power.draw",
	"power.limit",
}

// NVIDIA samples NVIDIA GPUs by shelling out to nvidia-smi, which avoids a cgo
// dependency on NVML.
type NVIDIA struct{ path string }

// NewNVIDIA returns a sampler if nvidia-smi is on PATH.
func NewNVIDIA() (*NVIDIA, bool) {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil, false
	}
	return &NVIDIA{path: path}, true
}

// Name implements Sampler.
func (n *NVIDIA) Name() string { return "NVIDIA" }

// Sample implements Sampler.
func (n *NVIDIA) Sample(ctx context.Context) ([]Device, error) {
	args := []string{
		"--query-gpu=" + strings.Join(nvidiaQuery, ","),
		"--format=csv,noheader,nounits",
	}
	out, err := exec.CommandContext(ctx, n.path, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi: %w", err)
	}

	r := csv.NewReader(strings.NewReader(string(out)))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi output: %w", err)
	}

	devices := make([]Device, 0, len(records))
	for _, rec := range records {
		if len(rec) < len(nvidiaQuery) {
			continue
		}
		d := Device{Vendor: "NVIDIA"}
		d.Index, _ = strconv.Atoi(strings.TrimSpace(rec[0]))
		d.Name = strings.TrimSpace(rec[1])

		d.UtilPercent, d.HasUtil = nvFloat(rec[2])

		usedMiB, okUsed := nvFloat(rec[3])
		totalMiB, okTotal := nvFloat(rec[4])
		if okUsed && okTotal {
			d.MemUsedBytes = uint64(usedMiB * 1024 * 1024)
			d.MemTotalBytes = uint64(totalMiB * 1024 * 1024)
			d.HasMem = true
		}

		d.TempCelsius, d.HasTemp = nvFloat(rec[5])

		draw, okDraw := nvFloat(rec[6])
		limit, _ := nvFloat(rec[7])
		if okDraw {
			d.PowerWatts = draw
			d.PowerLimitWatts = limit
			d.HasPower = true
		}

		devices = append(devices, d)
	}
	return devices, nil
}

// nvFloat parses a field, treating nvidia-smi's unsupported markers as absent.
func nvFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "[") || strings.EqualFold(s, "N/A") {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
