// Command vsfstoragebench runs the artifact-backend workloads against filesystem, SQLite and a MinIO server and prints a comparison.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/storage/bench"
)

type env struct {
	GoVersion string `json:"go"`
	CPUs      int    `json:"cpus"`
	CPUModel  string `json:"cpu_model"`
	Kernel    string `json:"kernel"`
	FSType    string `json:"filesystem"`
	MinIO     string `json:"minio,omitempty"`
}

func cpuModel() string {
	b, _ := os.ReadFile("/proc/cpuinfo")
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "model name" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func fsType(dir string) string {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return ""
	}
	return fmt.Sprintf("0x%x", st.Type)
}

func main() {
	var (
		targets   = flag.String("targets", "fs,sqlite,s3", "comma-separated: fs, sqlite, s3")
		one       = flag.String("one", "", "run one target in this process and print its report")
		child     = flag.Bool("child-writer", false, "internal: the process the crash test kills")
		target    = flag.String("target", "", "internal: target of the child writer")
		dir       = flag.String("dir", "", "working directory (default: a temporary one)")
		minio     = flag.String("minio", os.Getenv("VSF_MINIO"), "path of the minio server binary")
		out       = flag.String("out", "", "write the combined report as json here")
		seed      = flag.Uint64("seed", 7, "corpus seed")
		tiny      = flag.Int("tiny", 100000, "tiny objects")
		versions  = flag.Int("versions", 200, "similar large versions")
		vsize     = flag.Int("version-size", 512<<10, "bytes per large version")
		change    = flag.Float64("change-pct", 2, "percent of bytes that differ between versions")
		reads     = flag.Int("reads", 20000, "random tiny reads")
		workers   = flag.Int("workers", 8, "concurrent workers")
		mixedFor  = flag.Duration("mixed", 10*time.Second, "mixed workload duration")
		crashRuns = flag.Int("crash-runs", 3, "kill -9 runs per target")
		crashObjs = flag.Int("crash-objects", 20000, "objects the crash writer tries to store")
		objects   = flag.Int("objects", 0, "internal")
	)
	flag.Parse()
	ctx := context.Background()
	base := *dir
	if base == "" {
		d, err := os.MkdirTemp("", "vsfbench")
		fatal(err)
		base = d
		defer os.RemoveAll(base)
	}
	factory := func(name string) func(dir string) bench.Target {
		return func(d string) bench.Target {
			switch name {
			case "fs":
				return bench.NewFSTarget(d)
			case "sqlite":
				return bench.NewSQLiteTarget(d)
			case "s3":
				return bench.NewMinIOTarget(*minio, d)
			}
			fatal(fmt.Errorf("unknown target %q", name))
			return nil
		}
	}
	if *child {
		fatal(bench.ChildWriter(ctx, factory(*target)(*dir), *seed, *objects, 4))
		return
	}
	cfg := bench.Config{Seed: *seed, Tiny: *tiny, Versions: *versions, VersionSize: *vsize, ChangePct: *change, ReadSample: *reads, Workers: *workers, MixedFor: *mixedFor, DeleteShare: 0.5}
	if *one != "" {
		exe, _ := os.Executable()
		mk := factory(*one)
		t := mk(filepath.Join(base, "main"))
		var crash bench.CrashRunner
		if *crashRuns > 0 {
			if *one == "s3" {
				crash = bench.ServerCrash(*crashRuns, *seed, filepath.Join(base, "crash"), *crashObjs, mk)
			} else {
				crash = bench.ChildCrash(exe, *crashRuns, *seed, filepath.Join(base, "crash"), *crashObjs, mk, *one)
			}
		}
		rep, err := bench.Run(ctx, t, cfg, crash)
		fatal(err)
		fatal(json.NewEncoder(os.Stdout).Encode(rep))
		return
	}
	exe, _ := os.Executable()
	var reps []bench.Report
	for _, name := range strings.Split(*targets, ",") {
		name = strings.TrimSpace(name)
		if name == "s3" {
			if err := bench.Probe(*minio); err != nil {
				fmt.Fprintf(os.Stderr, "s3 skipped: %v (set -minio or VSF_MINIO)\n", err)
				continue
			}
		}
		args := []string{"-one", name, "-dir", filepath.Join(base, name), "-minio", *minio, "-seed", fmt.Sprint(*seed), "-tiny", fmt.Sprint(*tiny), "-versions", fmt.Sprint(*versions),
			"-version-size", fmt.Sprint(*vsize), "-change-pct", fmt.Sprint(*change), "-reads", fmt.Sprint(*reads), "-workers", fmt.Sprint(*workers), "-mixed", mixedFor.String(),
			"-crash-runs", fmt.Sprint(*crashRuns), "-crash-objects", fmt.Sprint(*crashObjs)}
		fmt.Fprintf(os.Stderr, "running %s ...\n", name)
		cmd := exec.Command(exe, args...)
		var stdout bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, os.Stderr
		fatal(cmd.Run())
		var rep bench.Report
		fatal(json.Unmarshal(stdout.Bytes(), &rep))
		reps = append(reps, rep)
	}
	e := env{GoVersion: runtime.Version(), CPUs: runtime.NumCPU(), CPUModel: cpuModel(), FSType: fsType(base)}
	if k, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		e.Kernel = strings.TrimSpace(string(k))
	}
	if *minio != "" {
		if o, err := exec.Command("go", "version", "-m", *minio).Output(); err == nil {
			for _, l := range strings.Split(string(o), "\n") {
				if strings.Contains(l, "\tmod\tgithub.com/minio/minio") {
					e.MinIO = strings.Join(strings.Fields(l)[1:], " ")
				}
			}
		}
	}
	fmt.Printf("environment: %+v\n\n%s", e, bench.Table(reps))
	if *out != "" {
		b, _ := json.MarshalIndent(map[string]any{"environment": e, "reports": reps}, "", "  ")
		fatal(os.WriteFile(*out, b, 0o644))
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "vsfstoragebench:", err)
		os.Exit(1)
	}
}
