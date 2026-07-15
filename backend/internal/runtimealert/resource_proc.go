package runtimealert

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

type linuxResourceSampler struct {
	mu        sync.Mutex
	lastTotal uint64
	lastIdle  uint64
}

func NewLinuxResourceSampler() ResourceSampler {
	return &linuxResourceSampler{}
}

func (s *linuxResourceSampler) Sample() (ResourceSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total, idle, err := readCPUStat("/proc/stat")
	if err != nil {
		return ResourceSample{}, err
	}
	memory, err := readMemoryPercent("/proc/meminfo")
	if err != nil {
		return ResourceSample{}, err
	}
	cpu := 0.0
	if s.lastTotal > 0 && total > s.lastTotal {
		totalDelta := total - s.lastTotal
		idleDelta := idle - s.lastIdle
		cpu = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
	}
	s.lastTotal = total
	s.lastIdle = idle
	return ResourceSample{CPUPercent: cpu, MemoryPercent: memory}, nil
}

func readCPUStat(path string) (uint64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0, 0, errors.New("/proc/stat missing cpu line")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, fmt.Errorf("invalid /proc/stat cpu line: %q", scanner.Text())
	}
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, 0, err
		}
		values = append(values, value)
	}
	total := uint64(0)
	for _, value := range values {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return total, idle, nil
}

func readMemoryPercent(path string) (float64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	values := map[string]float64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		if key != "MemTotal" && key != "MemAvailable" {
			continue
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, err
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	total := values["MemTotal"]
	available := values["MemAvailable"]
	if total <= 0 || available < 0 {
		return 0, errors.New("invalid /proc/meminfo values")
	}
	return (total - available) / total * 100, nil
}
