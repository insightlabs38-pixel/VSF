package bench

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/storage/blob"
)

// Target is one backend under test together with the process or files that hold its state.
type Target interface {
	Name() string
	// Open starts the backend over its directory, creating it if needed.
	Open(ctx context.Context) (blob.Backend, error)
	// Close stops it cleanly.
	Close() error
	// Kill stops it the way a crash would, when the target is a separate process.
	Kill() error
	// Pids are the processes whose memory and I/O belong to the backend.
	Pids() []int
	// SizeAtRest is the bytes the backend occupies on disk after flushing.
	SizeAtRest() (int64, error)
	// Maintain runs the backend's own compaction and returns how long it took.
	Maintain(ctx context.Context) (time.Duration, error)
	// External reports whether the backend lives in another process.
	External() bool
}

func dirSize(root string) (int64, error) {
	var n int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, e := d.Info(); e == nil {
				if st, ok := info.Sys().(*syscall.Stat_t); ok {
					n += st.Blocks * 512
				} else {
					n += info.Size()
				}
			}
		}
		return nil
	})
	return n, err
}

type fsTarget struct {
	dir string
	b   *blob.FS
}

func NewFSTarget(dir string) Target { return &fsTarget{dir: dir} }

func (t *fsTarget) Name() string   { return "filesystem" }
func (t *fsTarget) External() bool { return false }
func (t *fsTarget) Pids() []int    { return []int{os.Getpid()} }
func (t *fsTarget) Kill() error    { return fmt.Errorf("in-process") }
func (t *fsTarget) Open(ctx context.Context) (blob.Backend, error) {
	b, err := blob.NewFS(t.dir)
	t.b = b
	return b, err
}
func (t *fsTarget) Close() error {
	if t.b == nil {
		return nil
	}
	err := t.b.Close()
	t.b = nil
	return err
}
func (t *fsTarget) SizeAtRest() (int64, error) { return dirSize(t.dir) }
func (t *fsTarget) Maintain(context.Context) (time.Duration, error) {
	return 0, nil
}

type sqliteTarget struct {
	dir string
	b   *blob.SQLite
}

func NewSQLiteTarget(dir string) Target { return &sqliteTarget{dir: dir} }

func (t *sqliteTarget) Name() string   { return "sqlite" }
func (t *sqliteTarget) External() bool { return false }
func (t *sqliteTarget) Pids() []int    { return []int{os.Getpid()} }
func (t *sqliteTarget) Kill() error    { return fmt.Errorf("in-process") }
func (t *sqliteTarget) Open(ctx context.Context) (blob.Backend, error) {
	if err := os.MkdirAll(t.dir, 0o700); err != nil {
		return nil, err
	}
	b, err := blob.OpenSQLite(filepath.Join(t.dir, "objects.db"), "FULL")
	t.b = b
	return b, err
}
func (t *sqliteTarget) Close() error {
	if t.b == nil {
		return nil
	}
	err := t.b.Close()
	t.b = nil
	return err
}
func (t *sqliteTarget) SizeAtRest() (int64, error) {
	if t.b != nil {
		if err := t.b.Checkpoint(); err != nil {
			return 0, err
		}
	}
	return dirSize(t.dir)
}

// Maintain vacuums: the file only shrinks after deleted pages are rewritten.
func (t *sqliteTarget) Maintain(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	err := t.b.Vacuum()
	return time.Since(start), err
}

// MinIOTarget runs a real MinIO server as the commodity S3 store.
type MinIOTarget struct {
	Binary string
	dir    string
	port   int
	user   string
	pass   string
	cmd    *exec.Cmd
	s3     *blob.S3
}

func NewMinIOTarget(binary, dir string) *MinIOTarget {
	var r [12]byte
	_, _ = rand.Read(r[:])
	return &MinIOTarget{Binary: binary, dir: dir, user: "bench" + hex.EncodeToString(r[:4]), pass: hex.EncodeToString(r[:])}
}

func (t *MinIOTarget) Name() string   { return "s3-minio" }
func (t *MinIOTarget) External() bool { return true }
func (t *MinIOTarget) Pids() []int {
	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	return []int{t.cmd.Process.Pid}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (t *MinIOTarget) Open(ctx context.Context) (blob.Backend, error) {
	if err := os.MkdirAll(filepath.Join(t.dir, "data"), 0o700); err != nil {
		return nil, err
	}
	var err error
	if t.port == 0 {
		if t.port, err = freePort(); err != nil {
			return nil, err
		}
	}
	console, err := freePort()
	if err != nil {
		return nil, err
	}
	t.cmd = exec.Command(t.Binary, "server", filepath.Join(t.dir, "data"), "--address", fmt.Sprintf("127.0.0.1:%d", t.port), "--console-address", fmt.Sprintf("127.0.0.1:%d", console), "--quiet")
	t.cmd.Env = append(os.Environ(), "MINIO_ROOT_USER="+t.user, "MINIO_ROOT_PASSWORD="+t.pass, "MINIO_BROWSER=off", "MINIO_UPDATE=off")
	t.cmd.Dir = t.dir
	if err := t.cmd.Start(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/minio/health/ready", t.port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			_ = t.cmd.Process.Kill()
			return nil, fmt.Errorf("minio did not become ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.s3 = &blob.S3{Endpoint: fmt.Sprintf("http://127.0.0.1:%d", t.port), Bucket: "vsf", Region: "us-east-1", AccessKey: t.user, SecretKey: t.pass,
		Client: &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 64}, Timeout: 120 * time.Second}}
	if err := t.s3.CreateBucket(ctx); err != nil {
		return nil, err
	}
	return t.s3, nil
}

func (t *MinIOTarget) stop(sig syscall.Signal) error {
	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	_ = t.cmd.Process.Signal(sig)
	_, _ = t.cmd.Process.Wait()
	t.cmd = nil
	return nil
}

func (t *MinIOTarget) Close() error { return t.stop(syscall.SIGTERM) }
func (t *MinIOTarget) Kill() error  { return t.stop(syscall.SIGKILL) }

func (t *MinIOTarget) SizeAtRest() (int64, error) { return dirSize(filepath.Join(t.dir, "data")) }

func (t *MinIOTarget) Maintain(context.Context) (time.Duration, error) { return 0, nil }

// Probe says whether a MinIO binary is usable here.
func Probe(binary string) error {
	if binary == "" {
		return fmt.Errorf("no minio binary")
	}
	if _, err := os.Stat(binary); err != nil {
		return err
	}
	return nil
}
