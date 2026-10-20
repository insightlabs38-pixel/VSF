//go:build integration

package meta

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// pg is a throwaway PostgreSQL cluster on a unix socket, started as an unprivileged user.
type pg struct {
	t    *testing.T
	bin  string
	dir  string
	sock string
	as   []string
}

func pgBin(t *testing.T) string {
	if p := os.Getenv("VSF_POSTGRES_BIN"); p != "" {
		return p
	}
	m, _ := filepath.Glob("/usr/lib/postgresql/*/bin")
	if len(m) == 0 {
		t.Fatal("PostgreSQL binaries not found; set VSF_POSTGRES_BIN")
	}
	return m[len(m)-1]
}

func startPG(t *testing.T) *pg {
	t.Helper()
	p := &pg{t: t, bin: pgBin(t)}
	root, err := os.MkdirTemp("", "vsfpg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p.dir = filepath.Join(root, "data")
	p.sock = filepath.Join(root, "sock")
	if err := os.MkdirAll(p.sock, 0o755); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		p.as = []string{"runuser", "-u", "postgres", "--"}
		for _, d := range []string{root, p.sock} {
			if out, err := exec.Command("chown", "-R", "postgres", d).CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
		}
	}
	p.run("initdb", "-D", p.dir, "-A", "trust", "-U", "postgres", "--no-instructions")
	p.start()
	t.Cleanup(func() { _ = p.try("pg_ctl", "-D", p.dir, "-m", "immediate", "-w", "stop") })
	p.sql("postgres", "CREATE DATABASE vsf")
	return p
}

func (p *pg) try(name string, args ...string) error {
	argv := append(append([]string{}, p.as...), filepath.Join(p.bin, name))
	argv = append(argv, args...)
	cmd := exec.Command(argv[0], argv[1:]...)
	if len(p.as) == 0 {
		cmd = exec.Command(filepath.Join(p.bin, name), args...)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %v\n%s", name, err, out.String())
	}
	return nil
}

func (p *pg) run(name string, args ...string) {
	p.t.Helper()
	if err := p.try(name, args...); err != nil {
		p.t.Fatal(err)
	}
}

func (p *pg) start() {
	p.t.Helper()
	p.run("pg_ctl", "-D", p.dir, "-o", "-c listen_addresses='' -c unix_socket_directories="+p.sock+" -c fsync=on -c synchronous_commit=on", "-l", filepath.Join(p.dir, "log"), "-w", "start")
}

func (p *pg) crash() {
	p.t.Helper()
	p.run("pg_ctl", "-D", p.dir, "-m", "immediate", "-w", "stop")
	p.start()
}

// exec runs a script through psql; the error holds psql's message so a test can assert on it.
func (p *pg) exec(db, script string) (string, error) {
	cmd := exec.Command(filepath.Join(p.bin, "psql"), "-h", p.sock, "-U", "postgres", "-d", db, "-X", "-q", "-tA", "-v", "ON_ERROR_STOP=1")
	cmd.Stdin = strings.NewReader(script)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		return strings.TrimSpace(out.String()), fmt.Errorf("%v: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func (p *pg) sql(db, script string) string {
	p.t.Helper()
	out, err := p.exec(db, script)
	if err != nil {
		p.t.Fatalf("%v\n%s", err, script)
	}
	return out
}

func (p *pg) migrate(upTo int) error {
	script, err := PostgresScript(upTo)
	if err != nil {
		p.t.Fatal(err)
	}
	_, err = p.exec("vsf", script)
	return err
}

func asTenant(tenant, body string) string {
	return fmt.Sprintf("SET ROLE vsf_app;\nSELECT set_config('vsf.tenant', '%s', false);\n%s", tenant, body)
}

const d1 = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
const d2 = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
const d3 = "sha256:3333333333333333333333333333333333333333333333333333333333333333"

func seedTenant(p *pg, tenant string) {
	p.sql("vsf", fmt.Sprintf(`INSERT INTO tenants (id, created_at) VALUES ('%[1]s', 1);
INSERT INTO artifacts (tenant_id, digest, size, class, created_at) VALUES ('%[1]s', '%[2]s', 10, 'public', 1);
INSERT INTO manifests (tenant_id, id, kind, doc, key_id, created_at) VALUES ('%[1]s', '%[3]s', 'compiled', '\x6d'::bytea, 'k', 1);
INSERT INTO manifest_artifacts (tenant_id, manifest_id, artifact_digest) VALUES ('%[1]s', '%[3]s', '%[2]s');`, tenant, d1, d2))
}

func TestPostgresSchemaUpgradesKeepingDataAndRefusesDrift(t *testing.T) {
	p := startPG(t)
	if err := p.migrate(1); err != nil {
		t.Fatal(err)
	}
	seedTenant(p, "alpha")
	if err := p.migrate(0); err != nil {
		t.Fatalf("upgrade to the newest step: %v", err)
	}
	if got := p.sql("vsf", `SELECT max(version) FROM schema_migrations`); got != "2" {
		t.Fatalf("schema version %s", got)
	}
	if got := p.sql("vsf", `SELECT count(*) FROM manifests WHERE parent_id IS NULL AND tenant_id = 'alpha'`); got != "1" {
		t.Fatalf("data lost in the upgrade: %s", got)
	}
	if err := p.migrate(0); err != nil {
		t.Fatalf("running the script again must change nothing: %v", err)
	}
	if got := p.sql("vsf", `SELECT count(*) FROM schema_migrations`); got != "2" {
		t.Fatal(got)
	}
	p.sql("vsf", `UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1`)
	if err := p.migrate(0); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("a changed migration must stop the script: %v", err)
	}
}

func TestPostgresCommittedDataSurvivesACrashAndAnOpenTransactionDoesNot(t *testing.T) {
	p := startPG(t)
	if err := p.migrate(0); err != nil {
		t.Fatal(err)
	}
	seedTenant(p, "alpha")
	hold := exec.Command(filepath.Join(p.bin, "psql"), "-h", p.sock, "-U", "postgres", "-d", "vsf", "-X", "-q", "-v", "ON_ERROR_STOP=1")
	hold.Stdin = strings.NewReader("BEGIN;\nINSERT INTO tenants (id, created_at) VALUES ('uncommitted', 1);\nSELECT pg_sleep(30);\n")
	if err := hold.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	p.crash()
	_ = hold.Process.Kill()
	_, _ = hold.Process.Wait()
	if got := p.sql("vsf", `SELECT count(*) FROM tenants WHERE id = 'alpha'`); got != "1" {
		t.Fatalf("a committed tenant was lost: %s", got)
	}
	if got := p.sql("vsf", `SELECT count(*) FROM manifest_artifacts`); got != "1" {
		t.Fatalf("%s", got)
	}
	if got := p.sql("vsf", `SELECT count(*) FROM tenants WHERE id = 'uncommitted'`); got != "0" {
		t.Fatal("an open transaction must not survive a crash")
	}
}

func TestPostgresConcurrentSessionsClaimEachRootSequenceExactlyOnce(t *testing.T) {
	p := startPG(t)
	if err := p.migrate(0); err != nil {
		t.Fatal(err)
	}
	seedTenant(p, "alpha")
	const sessions, claims = 6, 15
	var wg sync.WaitGroup
	wins := make([]int, sessions)
	for s := 0; s < sessions; s++ {
		wg.Add(1)
		go func(s int) {
			defer wg.Done()
			for seq := 1; seq <= claims; seq++ {
				if _, err := p.exec("vsf", fmt.Sprintf(`INSERT INTO root_history (tenant_id, channel, seq, manifest_id, at) VALUES ('alpha', 'stable', %d, '%s', %d)`, seq, d2, s)); err == nil {
					wins[s]++
				} else if !strings.Contains(err.Error(), "duplicate key") {
					t.Errorf("session %d: %v", s, err)
					return
				}
			}
		}(s)
	}
	wg.Wait()
	total := 0
	for _, w := range wins {
		total += w
	}
	if total != claims {
		t.Fatalf("%d claims landed for %d sequence numbers", total, claims)
	}
	if got := p.sql("vsf", `SELECT count(DISTINCT seq) || '/' || max(seq) FROM root_history`); got != fmt.Sprintf("%d/%d", claims, claims) {
		t.Fatalf("history is not contiguous: %s", got)
	}
}

func TestPostgresRowLevelSecurityKeepsTenantsApart(t *testing.T) {
	p := startPG(t)
	if err := p.migrate(0); err != nil {
		t.Fatal(err)
	}
	seedTenant(p, "alpha")
	seedTenant(p, "beta")
	if got := p.sql("vsf", asTenant("alpha", `SELECT count(*) FROM artifacts;`)); !strings.HasSuffix(got, "1") {
		t.Fatalf("alpha sees %q", got)
	}
	if got := p.sql("vsf", asTenant("alpha", `SELECT count(*) FROM manifests WHERE tenant_id = 'beta';`)); !strings.HasSuffix(got, "0") {
		t.Fatalf("alpha reads beta's manifests: %q", got)
	}
	if got := p.sql("vsf", asTenant("alpha", `SELECT count(*) FROM tenants;`)); !strings.HasSuffix(got, "1") {
		t.Fatalf("tenants are scoped too: %q", got)
	}
	if got := p.sql("vsf", `SET ROLE vsf_app; SELECT count(*) FROM artifacts;`); !strings.HasSuffix(got, "0") {
		t.Fatalf("with no tenant set nothing is visible: %q", got)
	}
	if _, err := p.exec("vsf", asTenant("alpha", `INSERT INTO artifacts (tenant_id, digest, size, class, created_at) VALUES ('beta', '`+d3+`', 1, 'public', 1);`)); err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("writing into another tenant must be refused by policy: %v", err)
	}
	if got := p.sql("vsf", asTenant("alpha", `WITH d AS (DELETE FROM artifacts WHERE tenant_id = 'beta' RETURNING 1) SELECT count(*) FROM d;`)); !strings.HasSuffix(got, "0") {
		t.Fatalf("alpha deleted beta's rows: %q", got)
	}
	if got := p.sql("vsf", `SELECT count(*) FROM artifacts WHERE tenant_id = 'beta'`); got != "1" {
		t.Fatalf("beta lost a row: %s", got)
	}
	if _, err := p.exec("vsf", asTenant("alpha", `INSERT INTO manifest_artifacts (tenant_id, manifest_id, artifact_digest) VALUES ('alpha', '`+d2+`', '`+d3+`');`)); err == nil {
		t.Fatal("a link to an artifact the tenant does not have must be refused")
	}
	p.sql("vsf", `INSERT INTO artifacts (tenant_id, digest, size, class, created_at) VALUES ('beta', '`+d3+`', 1, 'public', 1)`)
	if _, err := p.exec("vsf", asTenant("alpha", `INSERT INTO manifest_artifacts (tenant_id, manifest_id, artifact_digest) VALUES ('alpha', '`+d2+`', '`+d3+`');`)); err == nil || !strings.Contains(err.Error(), "foreign key") {
		t.Fatalf("a composite key must stop a link to another tenant's artifact: %v", err)
	}
	if _, err := p.exec("vsf", asTenant("alpha", `DROP TABLE artifacts;`)); err == nil {
		t.Fatal("the application role must not alter the schema")
	}
	if _, err := p.exec("vsf", asTenant("alpha", `INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (99, 'x', 'x', 1);`)); err == nil {
		t.Fatal("the application role must not touch the migration history")
	}
}
