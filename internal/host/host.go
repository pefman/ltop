// Package host samples Linux CPU and memory usage from /proc.
package host

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Stats is a host resource snapshot.
type Stats struct {
	CPUPercent float64
	CPUCores   int

	MemTotalBytes     uint64
	MemAvailableBytes uint64
	SwapTotalBytes    uint64
	SwapFreeBytes     uint64

	LoadAvg1 float64
}

// MemUsedBytes is memory in use, excluding reclaimable cache.
func (s Stats) MemUsedBytes() uint64 {
	if s.MemTotalBytes < s.MemAvailableBytes {
		return 0
	}
	return s.MemTotalBytes - s.MemAvailableBytes
}

// MemPercent is memory occupancy in 0..1.
func (s Stats) MemPercent() float64 {
	if s.MemTotalBytes == 0 {
		return 0
	}
	return float64(s.MemUsedBytes()) / float64(s.MemTotalBytes)
}

// cpuTimes holds the aggregate jiffy counters from /proc/stat.
type cpuTimes struct{ total, idle float64 }

// Sampler computes CPU usage from the delta between successive /proc/stat reads.
type Sampler struct {
	prev  cpuTimes
	valid bool
}

// NewSampler returns a host sampler primed with an initial CPU reading.
func NewSampler() *Sampler {
	s := &Sampler{}
	if t, ok := readCPUTimes(); ok {
		s.prev, s.valid = t, true
	}
	return s
}

// Sample reads current host statistics. The first call reports zero CPU usage
// because a delta needs two readings.
func (s *Sampler) Sample() Stats {
	stats := Stats{CPUCores: numCPU()}

	if t, ok := readCPUTimes(); ok {
		if s.valid {
			totalDelta := t.total - s.prev.total
			idleDelta := t.idle - s.prev.idle
			if totalDelta > 0 {
				stats.CPUPercent = clamp01((totalDelta-idleDelta)/totalDelta) * 100
			}
		}
		s.prev, s.valid = t, true
	}

	readMeminfo(&stats)
	stats.LoadAvg1 = readLoadAvg1()
	return stats
}

func readCPUTimes() (cpuTimes, bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, f := range fields[1:] {
			v, err := strconv.ParseFloat(f, 64)
			if err != nil {
				continue
			}
			t.total += v
			// Columns 3 and 4 are idle and iowait.
			if i == 3 || i == 4 {
				t.idle += v
			}
		}
		return t, t.total > 0
	}
	return cpuTimes{}, false
}

func readMeminfo(stats *Stats) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		// Values are reported in kibibytes.
		kib, err := strconv.ParseUint(strings.Fields(strings.TrimSpace(value))[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			stats.MemTotalBytes = kib * 1024
		case "MemAvailable":
			stats.MemAvailableBytes = kib * 1024
		case "SwapTotal":
			stats.SwapTotalBytes = kib * 1024
		case "SwapFree":
			stats.SwapFreeBytes = kib * 1024
		}
	}
}

func readLoadAvg1() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func numCPU() int {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()

	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			n++
		}
	}
	return n
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
