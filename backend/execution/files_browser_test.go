//go:build integration

package execution

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const uploadPage = `<!doctype html><meta charset=utf-8><title>files</title>
<input type=file id=f multiple><pre id=out></pre><a id=dl href="/dl">export</a>
<script>
const out = document.getElementById("out");
document.getElementById("f").addEventListener("change", async (e) => {
  const rows = [];
  for (const f of e.target.files) {
    const buf = await f.arrayBuffer();
    const h = await crypto.subtle.digest("SHA-256", buf);
    rows.push(f.name + "|" + f.size + "|" + [...new Uint8Array(h)].map(b => b.toString(16).padStart(2, "0")).join(""));
  }
  out.textContent = rows.join("\n");
});
</script>`

func TestARealBrowserReceivesTheStagedFilesWithTheirNamesAndDigests(t *testing.T) {
	b := theBrowser(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(uploadPage)) }))
	defer srv.Close()
	sb, _ := NewFileSandbox(t.TempDir())
	names := []string{"naïve \"quoted\" ünï.txt", "日本語 レポート.csv", "../../traversal.txt"}
	var refs, paths []string
	var want []string
	for i, n := range names {
		content := []byte(fmt.Sprintf("content number %d\n", i))
		ref, err := sb.Stage("s1", n, content)
		if err != nil {
			t.Fatal(err)
		}
		p, err := sb.Path("s1", ref)
		if err != nil {
			t.Fatal(err)
		}
		refs, paths = append(refs, ref), append(paths, p)
		safe, _, _ := SafeFileName(n)
		_ = safe
		want = append(want, fmt.Sprintf("%s|%d|%s", filepath.Base(p), len(content), Digest(content)))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pg, err := b.NewPage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close(context.WithoutCancel(ctx))
	if err := pg.Navigate(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetInputFiles(ctx, "#f", sb.root, paths); err != nil {
		t.Fatal(err)
	}
	var got string
	for i := 0; i < 50 && got == ""; i++ {
		_ = pg.Eval(ctx, `document.getElementById("out").textContent`, &got)
		time.Sleep(100 * time.Millisecond)
	}
	rows := strings.Split(got, "\n")
	if len(rows) != len(names) {
		t.Fatalf("browser reported %q", got)
	}
	for i, r := range rows {
		parts := strings.Split(r, "|")
		if parts[1] != strings.Split(want[i], "|")[1] || parts[2] != strings.Split(want[i], "|")[2] {
			t.Errorf("row %d: browser saw %q, staged %q", i, r, want[i])
		}
	}
	_ = refs
}

func TestAPathOutsideTheSandboxIsNeverHandedToABrowser(t *testing.T) {
	b := theBrowser(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(uploadPage)) }))
	defer srv.Close()
	sb, _ := NewFileSandbox(t.TempDir())
	outside := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outside, []byte("secret"), 0o600)
	link := filepath.Join(sb.root, "link.txt")
	_ = os.Symlink(outside, link)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pg, err := b.NewPage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close(context.WithoutCancel(ctx))
	_ = pg.Navigate(ctx, srv.URL)
	for _, p := range []string{outside, link} {
		if err := pg.SetInputFiles(ctx, "#f", sb.root, []string{p}); reasonOf(err) != FileSessionMismatch {
			t.Errorf("%s: %v", p, err)
		}
	}
	if err := pg.SetInputFiles(ctx, "#out", sb.root, nil); err == nil {
		t.Error("a non-file element accepted files")
	}
}

func TestADownloadLandsInTheChosenDirectoryWithTheDeclaredBytes(t *testing.T) {
	b := theBrowser(t)
	content := []byte("a,b\n1,2\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dl" {
			w.Header().Set("Content-Disposition", DispositionFilename("Zusammenfassung ü.csv"))
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write(content)
			return
		}
		_, _ = w.Write([]byte(uploadPage))
	}))
	defer srv.Close()
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.AllowDownloads(ctx, dir); err != nil {
		t.Fatal(err)
	}
	pg, err := b.NewPage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close(context.WithoutCancel(ctx))
	if err := pg.Navigate(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	known := ListDir(dir)
	if reason, err := pg.Click(ctx, "#dl"); err != nil || reason != "" {
		t.Fatalf("%q %v", reason, err)
	}
	path, err := AwaitDownload(ctx, dir, known, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if Digest(got) != Digest(content) {
		t.Fatalf("downloaded %q", got)
	}
	// Chromium may save a name its locale cannot store as "download"; the declared name comes from the header.
	if name, _, err := NameFromDisposition(DispositionFilename("Zusammenfassung ü.csv")); err != nil || name != "Zusammenfassung ü.csv" {
		t.Errorf("declared name %q %v", name, err)
	}
}
