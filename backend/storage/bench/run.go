package bench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
)

// Config sizes one run; every target sees the same seeded corpus.
type Config struct {
	Seed        uint64
	Tiny        int
	Versions    int
	VersionSize int
	ChangePct   float64
	ReadSample  int
	Workers     int
	MixedFor    time.Duration
	// DeleteShare is the fraction of tiny objects removed in the compaction phase.
	DeleteShare float64
}

func Defaults() Config {
	return Config{Seed: 7, Tiny: 100000, Versions: 200, VersionSize: 512 << 10, ChangePct: 2, ReadSample: 20000, Workers: 8, MixedFor: 10 * time.Second, DeleteShare: 0.5}
}

// Phase is one measured stretch of work.
type Phase struct {
	Name         string  `json:"name"`
	Objects      int     `json:"objects"`
	LogicalBytes int64   `json:"logical_bytes"`
	Seconds      float64 `json:"seconds"`
	OpsPerSec    float64 `json:"ops_per_sec"`
	MBPerSec     float64 `json:"mb_per_sec"`
	Latency      Latency `json:"latency"`
	IO           ProcIO  `json:"io"`
	Errors       int     `json:"errors"`
}

// Report is everything measured for one target.
type Report struct {
	Target          string   `json:"target"`
	Config          Config   `json:"config"`
	Phases          []Phase  `json:"phases"`
	SizeTinyBytes   int64    `json:"size_after_tiny_bytes"`
	SizeVersions    int64    `json:"size_after_versions_bytes"`
	TinyLogical     int64    `json:"tiny_logical_bytes"`
	VersionsLogical int64    `json:"versions_logical_bytes"`
	ColdStartMs     int64    `json:"cold_start_ms"`
	PeakRSSKiB      int64    `json:"peak_rss_kib"`
	BaselineRSSKiB  int64    `json:"baseline_rss_kib"`
	Crash           []Crash  `json:"crash"`
	Compaction      Compact  `json:"compaction"`
	Dedup           DedupRep `json:"chunk_dedup_potential"`
	Notes           []string `json:"notes,omitempty"`
}

type Compact struct {
	Deleted        int     `json:"deleted"`
	SizeBefore     int64   `json:"size_before_bytes"`
	SizeAfterDel   int64   `json:"size_after_delete_bytes"`
	SizeAfterMaint int64   `json:"size_after_maintenance_bytes"`
	DeleteSeconds  float64 `json:"delete_seconds"`
	MaintSeconds   float64 `json:"maintenance_seconds"`
}

type DedupRep struct {
	AvgChunk      int     `json:"avg_chunk_bytes"`
	Logical       int64   `json:"logical_bytes"`
	Unique        int64   `json:"unique_bytes"`
	SavedFraction float64 `json:"saved_fraction"`
}

type Crash struct {
	Run           int   `json:"run"`
	Acked         int   `json:"acked"`
	AckedLost     int   `json:"acked_lost"`
	AckedCorrupt  int   `json:"acked_corrupt"`
	PresentTorn   int   `json:"present_but_torn"`
	Unacked       int   `json:"present_unacked"`
	RecoveryMs    int64 `json:"recovery_ms"`
	KillAfterMs   int64 `json:"kill_after_ms"`
	ReopenFailure bool  `json:"reopen_failure,omitempty"`
}

func pidsIO(t Target) ProcIO {
	var sum ProcIO
	for _, p := range t.Pids() {
		x := IOOf(p)
		sum.WriteBytes += x.WriteBytes
		sum.ReadBytes += x.ReadBytes
		sum.SyscW += x.SyscW
	}
	return sum
}

func peakRSS(t Target) int64 {
	var n int64
	for _, p := range t.Pids() {
		h, _ := RSSOf(p)
		n += h
	}
	return n
}

func curRSS(t Target) int64 {
	var n int64
	for _, p := range t.Pids() {
		_, c := RSSOf(p)
		n += c
	}
	return n
}

func finish(ph *Phase, d []time.Duration, took time.Duration, before ProcIO, t Target) {
	ph.Seconds = took.Seconds()
	ph.Latency = Summarize(d)
	if ph.Seconds > 0 {
		ph.OpsPerSec = float64(ph.Objects) / ph.Seconds
		ph.MBPerSec = float64(ph.LogicalBytes) / (1 << 20) / ph.Seconds
	}
	ph.IO = pidsIO(t).Sub(before)
}

// writeAll writes objects with workers concurrent writers and records each Put's latency.
func writeAll(ctx context.Context, name string, b blob.Backend, t Target, objs []Object, workers int) Phase {
	ph := Phase{Name: name, Objects: len(objs)}
	for _, o := range objs {
		ph.LogicalBytes += int64(len(o.Data))
	}
	lat := make([]time.Duration, len(objs))
	var next, errs atomic.Int64
	before := pidsIO(t)
	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(objs) {
					return
				}
				t0 := time.Now()
				if err := b.Put(ctx, objs[i].Key, objs[i].Data); err != nil {
					errs.Add(1)
				}
				lat[i] = time.Since(t0)
			}
		}()
	}
	wg.Wait()
	ph.Errors = int(errs.Load())
	finish(&ph, lat, time.Since(start), before, t)
	return ph
}

func readSample(ctx context.Context, name string, b blob.Backend, t Target, objs []Object, order []int, n, workers int) Phase {
	if n > len(order) {
		n = len(order)
	}
	ph := Phase{Name: name, Objects: n}
	lat := make([]time.Duration, n)
	var next, errs atomic.Int64
	var logical atomic.Int64
	before := pidsIO(t)
	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				o := objs[order[i]]
				t0 := time.Now()
				got, err := b.Get(ctx, o.Key)
				lat[i] = time.Since(t0)
				if err != nil || !bytes.Equal(got, o.Data) {
					errs.Add(1)
					continue
				}
				logical.Add(int64(len(got)))
			}
		}()
	}
	wg.Wait()
	ph.LogicalBytes = logical.Load()
	ph.Errors = int(errs.Load())
	finish(&ph, lat, time.Since(start), before, t)
	return ph
}

func mixed(ctx context.Context, b blob.Backend, t Target, objs, fresh []Object, workers int, d time.Duration, seed uint64) Phase {
	ph := Phase{Name: "mixed 70% read 30% write"}
	var mu sync.Mutex
	var lat []time.Duration
	var errs, ops, bytesMoved atomic.Int64
	var writes atomic.Int64
	before := pidsIO(t)
	start := time.Now()
	deadline := start.Add(d)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := newRng(seed + uint64(w)*977)
			var mine []time.Duration
			for time.Now().Before(deadline) {
				t0 := time.Now()
				if r.intn(100) < 70 && len(objs) > 0 {
					o := objs[r.intn(len(objs))]
					got, err := b.Get(ctx, o.Key)
					if err != nil || !bytes.Equal(got, o.Data) {
						errs.Add(1)
					} else {
						bytesMoved.Add(int64(len(got)))
					}
				} else {
					i := int(writes.Add(1)) - 1
					if i >= len(fresh) {
						writes.Add(-1)
						continue
					}
					if err := b.Put(ctx, fresh[i].Key, fresh[i].Data); err != nil {
						errs.Add(1)
					} else {
						bytesMoved.Add(int64(len(fresh[i].Data)))
					}
				}
				mine = append(mine, time.Since(t0))
				ops.Add(1)
			}
			mu.Lock()
			lat = append(lat, mine...)
			mu.Unlock()
		}(w)
	}
	wg.Wait()
	ph.Objects = int(ops.Load())
	ph.LogicalBytes = bytesMoved.Load()
	ph.Errors = int(errs.Load())
	finish(&ph, lat, time.Since(start), before, t)
	return ph
}

func verifyDigest(key string, data []byte) bool {
	i := strings.LastIndex(key, "/")
	sum := sha256.Sum256(data)
	return key[i+1:] == hex.EncodeToString(sum[:])
}

// Run executes every workload against one target in this process.
func Run(ctx context.Context, t Target, cfg Config, crash CrashRunner) (Report, error) {
	rep := Report{Target: t.Name(), Config: cfg}
	tiny := Tiny(cfg.Seed, cfg.Tiny)
	fresh := Tiny(cfg.Seed+1, cfg.Tiny/4+1000)
	vers := Versions(cfg.Seed+2, cfg.Versions, cfg.VersionSize, cfg.ChangePct)
	for _, o := range tiny {
		rep.TinyLogical += int64(len(o.Data))
	}
	for _, o := range vers {
		rep.VersionsLogical += int64(len(o.Data))
	}
	rep.BaselineRSSKiB = curRSS(t)
	b, err := t.Open(ctx)
	if err != nil {
		return rep, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = t.Close()
		}
	}()
	rep.BaselineRSSKiB = curRSS(t)

	rep.Phases = append(rep.Phases, writeAll(ctx, "write tiny objects", b, t, tiny, cfg.Workers))
	if rep.SizeTinyBytes, err = t.SizeAtRest(); err != nil {
		return rep, err
	}
	order := Shuffled(cfg.Seed, len(tiny))
	rep.Phases = append(rep.Phases, readSample(ctx, "read tiny objects (random, warm)", b, t, tiny, order, cfg.ReadSample, cfg.Workers))
	rep.Phases = append(rep.Phases, writeAll(ctx, "write similar large versions", b, t, vers, 2))
	if rep.SizeVersions, err = t.SizeAtRest(); err != nil {
		return rep, err
	}
	vorder := Shuffled(cfg.Seed+3, len(vers))
	rep.Phases = append(rep.Phases, readSample(ctx, "read large versions (random)", b, t, vers, vorder, len(vers), 2))
	rep.Phases = append(rep.Phases, mixed(ctx, b, t, tiny, fresh, cfg.Workers, cfg.MixedFor, cfg.Seed))

	deleteN := int(float64(len(tiny)) * cfg.DeleteShare)
	rep.Compaction.Deleted = deleteN
	rep.Compaction.SizeBefore, _ = t.SizeAtRest()
	t0 := time.Now()
	for _, i := range Shuffled(cfg.Seed+4, len(tiny))[:deleteN] {
		if err := b.Delete(ctx, tiny[i].Key); err != nil && !errors.Is(err, blob.ErrNotFound) {
			rep.Notes = append(rep.Notes, "delete: "+err.Error())
			break
		}
	}
	rep.Compaction.DeleteSeconds = time.Since(t0).Seconds()
	rep.Compaction.SizeAfterDel, _ = t.SizeAtRest()
	d, merr := t.Maintain(ctx)
	if merr != nil {
		rep.Notes = append(rep.Notes, "maintenance: "+merr.Error())
	}
	rep.Compaction.MaintSeconds = d.Seconds()
	rep.Compaction.SizeAfterMaint, _ = t.SizeAtRest()
	rep.PeakRSSKiB = peakRSS(t)

	if err := t.Close(); err != nil {
		return rep, err
	}
	closed = true
	start := time.Now()
	b2, err := t.Open(ctx)
	if err != nil {
		return rep, fmt.Errorf("reopen: %w", err)
	}
	probe := vers[0]
	got, gerr := b2.Get(ctx, probe.Key)
	rep.ColdStartMs = time.Since(start).Milliseconds()
	if gerr != nil || !bytes.Equal(got, probe.Data) {
		rep.Notes = append(rep.Notes, "the first read after a restart failed")
	}
	_ = t.Close()

	if crash != nil {
		cr, err := crash(ctx, t, tiny)
		if err != nil {
			rep.Notes = append(rep.Notes, "crash test: "+err.Error())
		}
		rep.Crash = cr
	}
	avg := 16 << 10
	rep.Dedup.AvgChunk = avg
	rep.Dedup.Logical, rep.Dedup.Unique = DedupBytes(vers, avg)
	if rep.Dedup.Logical > 0 {
		rep.Dedup.SavedFraction = 1 - float64(rep.Dedup.Unique)/float64(rep.Dedup.Logical)
	}
	return rep, nil
}

// CrashRunner kills the target in the middle of acknowledged writes and checks what survived.
type CrashRunner func(ctx context.Context, t Target, corpus []Object) ([]Crash, error)
