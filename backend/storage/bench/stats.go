// Package bench measures object backends under one seeded workload so their numbers can be compared.
package bench

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Latency summarizes a set of operation durations.
type Latency struct {
	N                  int     `json:"n"`
	P50, P95, P99, Max float64 `json:"-"`
	P50us              int64   `json:"p50_us"`
	P95us              int64   `json:"p95_us"`
	P99us              int64   `json:"p99_us"`
	Maxus              int64   `json:"max_us"`
	MeanUs             int64   `json:"mean_us"`
}

func Summarize(d []time.Duration) Latency {
	if len(d) == 0 {
		return Latency{}
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	at := func(q float64) int64 {
		i := int(q * float64(len(s)))
		if i >= len(s) {
			i = len(s) - 1
		}
		return s[i].Microseconds()
	}
	var sum time.Duration
	for _, v := range s {
		sum += v
	}
	return Latency{N: len(s), P50us: at(.50), P95us: at(.95), P99us: at(.99), Maxus: s[len(s)-1].Microseconds(), MeanUs: (sum / time.Duration(len(s))).Microseconds()}
}

// ProcIO is what the kernel says a process wrote to and read from storage.
type ProcIO struct {
	WriteBytes int64 `json:"write_bytes"`
	ReadBytes  int64 `json:"read_bytes"`
	SyscW      int64 `json:"syscalls_write"`
}

func readKV(path string) map[string]int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	m := map[string]int64{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		if n, err := strconv.ParseInt(f[0], 10, 64); err == nil {
			m[strings.TrimSpace(k)] = n
		}
	}
	return m
}

// IOOf reads /proc/<pid>/io; it needs the same user or root.
func IOOf(pid int) ProcIO {
	m := readKV(fmt.Sprintf("/proc/%d/io", pid))
	return ProcIO{WriteBytes: m["write_bytes"], ReadBytes: m["read_bytes"], SyscW: m["syscw"]}
}

// RSSOf is the peak resident set (VmHWM) and current RSS in KiB.
func RSSOf(pid int) (peakKiB, curKiB int64) {
	m := readKV(fmt.Sprintf("/proc/%d/status", pid))
	return m["VmHWM"], m["VmRSS"]
}

func (a ProcIO) Sub(b ProcIO) ProcIO {
	return ProcIO{WriteBytes: a.WriteBytes - b.WriteBytes, ReadBytes: a.ReadBytes - b.ReadBytes, SyscW: a.SyscW - b.SyscW}
}
