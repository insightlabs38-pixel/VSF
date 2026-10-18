// Command vsfbench measures how a Go service can invoke the native Fabric worker.
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"syscall"
	"time"
)

type result struct {
	Mode       string  `json:"mode"`
	Iterations int     `json:"iterations"`
	PackBytes  int     `json:"pack_bytes"`
	P50Ms      float64 `json:"p50_ms"`
	P95Ms      float64 `json:"p95_ms"`
	P99Ms      float64 `json:"p99_ms"`
	MaxMs      float64 `json:"max_ms"`
	ChildRSSKB int64   `json:"child_peak_rss_kb"`
	RestartMs  float64 `json:"restart_after_kill_ms,omitempty"`
	KillError  string  `json:"error_after_kill,omitempty"`
}

func percentile(sorted []float64, p int) float64 {
	rank := (p*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

func summarize(mode string, packBytes int, samples []float64, rss int64) result {
	sort.Float64s(samples)
	return result{Mode: mode, Iterations: len(samples), PackBytes: packBytes,
		P50Ms: percentile(samples, 50), P95Ms: percentile(samples, 95), P99Ms: percentile(samples, 99),
		MaxMs: samples[len(samples)-1], ChildRSSKB: rss}
}

func maxrss(ps *os.ProcessState) int64 {
	if ru, ok := ps.SysUsage().(*syscall.Rusage); ok {
		return ru.Maxrss
	}
	return 0
}

func spawn(bin string, pack []byte, n int) (result, error) {
	var samples []float64
	var rss int64
	for i := 0; i < n; i++ {
		cmd := exec.Command(bin, "pack-analyze", "-")
		cmd.Stdin = bytes.NewReader(pack)
		var out bytes.Buffer
		cmd.Stdout = &out
		start := time.Now()
		if err := cmd.Run(); err != nil {
			return result{}, err
		}
		samples = append(samples, float64(time.Since(start).Microseconds())/1000)
		if r := maxrss(cmd.ProcessState); r > rss {
			rss = r
		}
	}
	return summarize("go-spawn-per-call", len(pack), samples, rss), nil
}

type worker struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func start(bin string) (*worker, error) {
	cmd := exec.Command(bin, "serve")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &worker{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20)}, nil
}

func (w *worker) call(op byte, payload []byte) (byte, []byte, error) {
	frame := make([]byte, 4, 5+len(payload))
	binary.LittleEndian.PutUint32(frame, uint32(len(payload)+1))
	frame = append(frame, op)
	frame = append(frame, payload...)
	if _, err := w.in.Write(frame); err != nil {
		return 0, nil, err
	}
	head := make([]byte, 5)
	if _, err := io.ReadFull(w.out, head); err != nil {
		return 0, nil, err
	}
	body := make([]byte, binary.LittleEndian.Uint32(head[1:]))
	if _, err := io.ReadFull(w.out, body); err != nil {
		return 0, nil, err
	}
	return head[0], body, nil
}

func persistent(bin string, pack []byte, n int) (result, error) {
	w, err := start(bin)
	if err != nil {
		return result{}, err
	}
	var samples []float64
	for i := 0; i < n; i++ {
		begin := time.Now()
		status, _, err := w.call('A', pack)
		if err != nil || status != 0 {
			return result{}, fmt.Errorf("call failed: %v status %d", err, status)
		}
		samples = append(samples, float64(time.Since(begin).Microseconds())/1000)
	}
	w.in.Close()
	w.cmd.Wait()
	res := summarize("go-persistent-worker", len(pack), samples, maxrss(w.cmd.ProcessState))

	// Failure mode: SIGKILL the worker, observe the error, then measure restart plus first call.
	w2, err := start(bin)
	if err != nil {
		return res, nil
	}
	if _, _, err := w2.call('A', pack); err != nil {
		return res, nil
	}
	w2.cmd.Process.Kill()
	w2.cmd.Wait()
	if _, _, err := w2.call('A', pack); err != nil {
		res.KillError = "write/read failed after kill (detected)"
	}
	begin := time.Now()
	w3, err := start(bin)
	if err == nil {
		if status, _, err := w3.call('A', pack); err == nil && status == 0 {
			res.RestartMs = float64(time.Since(begin).Microseconds()) / 1000
		}
		w3.in.Close()
		w3.cmd.Wait()
	}
	return res, nil
}

func main() {
	bin := flag.String("bin", "", "path to vsf-fabric")
	packPath := flag.String("pack", "", "pack file")
	n := flag.Int("iterations", 50, "iterations")
	flag.Parse()
	pack, err := os.ReadFile(*packPath)
	if err != nil || *bin == "" {
		fmt.Fprintln(os.Stderr, "usage: vsfbench -bin PATH -pack FILE [-iterations N]")
		os.Exit(64)
	}
	var results []result
	for _, f := range []func(string, []byte, int) (result, error){spawn, persistent} {
		r, err := f(*bin, pack, *n)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		results = append(results, r)
	}
	json.NewEncoder(os.Stdout).Encode(results)
}
