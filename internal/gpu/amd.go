package gpu

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// amdVendorID is the PCI vendor ID AMD graphics devices report in sysfs.
const amdVendorID = "0x1002"

// cardName matches primary DRM nodes, excluding connector entries like card0-DP-1.
var cardName = regexp.MustCompile(`^card\d+$`)

// AMD samples AMD GPUs from the amdgpu sysfs interface. sysfs is preferred over
// rocm-smi because it needs no ROCm installation and no subprocess per poll.
type AMD struct{ cards []string }

// NewAMD returns a sampler if at least one amdgpu card exposes sysfs counters.
func NewAMD() (*AMD, bool) {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return nil, false
	}
	var cards []string
	for _, e := range entries {
		if !cardName.MatchString(e.Name()) {
			continue
		}
		dev := filepath.Join("/sys/class/drm", e.Name(), "device")
		if strings.TrimSpace(readFile(filepath.Join(dev, "vendor"))) != amdVendorID {
			continue
		}
		// Only cards exposing a busy counter are usable render GPUs.
		if _, err := os.Stat(filepath.Join(dev, "gpu_busy_percent")); err != nil {
			continue
		}
		cards = append(cards, dev)
	}
	if len(cards) == 0 {
		return nil, false
	}
	return &AMD{cards: cards}, true
}

// Name implements Sampler.
func (a *AMD) Name() string { return "AMD" }

// Sample implements Sampler.
func (a *AMD) Sample(ctx context.Context) ([]Device, error) {
	devices := make([]Device, 0, len(a.cards))
	for i, dev := range a.cards {
		if err := ctx.Err(); err != nil {
			return devices, err
		}
		d := Device{Index: i, Vendor: "AMD", Name: amdName(dev)}

		if v, ok := readFloat(filepath.Join(dev, "gpu_busy_percent")); ok {
			d.UtilPercent, d.HasUtil = v, true
		}
		used, okUsed := readFloat(filepath.Join(dev, "mem_info_vram_used"))
		total, okTotal := readFloat(filepath.Join(dev, "mem_info_vram_total"))
		if okUsed && okTotal {
			d.MemUsedBytes = uint64(used)
			d.MemTotalBytes = uint64(total)
			d.HasMem = true
		}

		if hw := hwmonDir(dev); hw != "" {
			// Temperatures are exposed in millidegrees Celsius.
			if v, ok := readFloat(filepath.Join(hw, "temp1_input")); ok {
				d.TempCelsius, d.HasTemp = v/1000, true
			}
			// Power is exposed in microwatts.
			if v, ok := firstFloat(hw, "power1_average", "power1_input"); ok {
				d.PowerWatts, d.HasPower = v/1e6, true
			}
			if v, ok := firstFloat(hw, "power1_cap", "power1_crit"); ok {
				d.PowerLimitWatts = v / 1e6
			}
		}
		devices = append(devices, d)
	}
	if len(devices) == 0 {
		return nil, errors.New("no amdgpu devices readable")
	}
	return devices, nil
}

// amdName prefers the marketing name exposed by newer amdgpu builds and falls
// back to the PCI device ID.
func amdName(dev string) string {
	for _, f := range []string{"product_name", "device_name"} {
		if v := strings.TrimSpace(readFile(filepath.Join(dev, f))); v != "" {
			return v
		}
	}
	if id := strings.TrimSpace(readFile(filepath.Join(dev, "device"))); id != "" {
		return "AMD " + id
	}
	return "AMD GPU"
}

func hwmonDir(dev string) string {
	matches, err := filepath.Glob(filepath.Join(dev, "hwmon", "hwmon*"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func firstFloat(dir string, names ...string) (float64, bool) {
	for _, n := range names {
		if v, ok := readFloat(filepath.Join(dir, n)); ok {
			return v, true
		}
	}
	return 0, false
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func readFloat(path string) (float64, bool) {
	s := strings.TrimSpace(readFile(path))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
