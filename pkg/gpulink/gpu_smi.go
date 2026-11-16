package gpulink

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// smiQuery is what nvidia-smi is asked for, in column order.
const smiQuery = "index,name,memory.total,memory.used,utilization.gpu,utilization.memory,temperature.gpu,power.draw,power.limit,clocks.sm,fan.speed"

// smiTimeout bounds one nvidia-smi run.
const smiTimeout = 3 * time.Second

// runSMI runs nvidia-smi; a variable so tests can stand in for it.
var runSMI = func(ctx context.Context) ([]byte, error) {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, "--query-gpu="+smiQuery, "--format=csv,noheader,nounits").Output() //nolint:gosec // G204: fixed binary and arguments
}

// gpusFromSMI samples the machine's NVIDIA GPUs. nil when there is no
// nvidia-smi or it fails: GPU hardware metrics are then simply absent.
func gpusFromSMI(ctx context.Context) []GPUStats {
	ctx, cancel := context.WithTimeout(ctx, smiTimeout)
	defer cancel()
	out, err := runSMI(ctx)
	if err != nil {
		return nil
	}
	return parseSMI(string(out))
}

// parseSMI reads nvidia-smi's CSV (noheader, nounits) in smiQuery's order.
// "[N/A]" and other unreadable values are left absent.
func parseSMI(out string) []GPUStats {
	var gpus []GPUStats
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		cols := strings.Split(line, ",")
		if len(cols) < 11 {
			continue
		}
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		idx, err := parseIndex(cols[0])
		if err != nil {
			continue
		}
		num := func(s string) *float64 {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil
			}
			return ptr(v)
		}
		mib := func(s string) *float64 {
			if v := num(s); v != nil {
				return ptr(*v * 1024 * 1024)
			}
			return nil
		}
		gpus = append(gpus, GPUStats{
			Index:            idx,
			Name:             cols[1],
			MemoryTotalBytes: mib(cols[2]),
			MemoryUsedBytes:  mib(cols[3]),
			UtilizationPct:   num(cols[4]),
			MemoryUtilPct:    num(cols[5]),
			TemperatureC:     num(cols[6]),
			PowerW:           num(cols[7]),
			PowerLimitW:      num(cols[8]),
			SMClockMHz:       num(cols[9]),
			FanPct:           num(cols[10]),
			Source:           "nvidia-smi",
		})
	}
	return gpus
}

func parseIndex(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}
