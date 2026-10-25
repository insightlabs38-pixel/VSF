package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSafeFileNameKeepsPlainNamesAndNeutralizesTheRest(t *testing.T) {
	long := strings.Repeat("é", 200) + ".txt"
	for _, c := range []struct {
		in, want string
		changed  bool
		bad      bool
	}{
		{"report.pdf", "report.pdf", false, false},
		{"my file (1).txt", "my file (1).txt", false, false},
		{"日本語.csv", "日本語.csv", false, false},
		{"../../etc/passwd", "passwd", true, false},
		{`C:\Users\x\a.txt`, "a.txt", true, false},
		{"a\x07b.txt", "a_b.txt", true, false},
		{"evil\u202egpj.exe", "evil_gpj.exe", true, false},
		{`a<b>:"c|d?e*.txt`, "a_b___c_d_e_.txt", true, false},
		{"CON.txt", "_CON.txt", true, false},
		{"trailing. . ", "trailing", true, false},
		{"café.txt", "café.txt", false, false}, // composed form stays as given
		{"", "", false, true},
		{"..", "", false, true},
		{"...", "", false, true},
		{"a\x00b", "", false, true},
		{"\xff\xfe.txt", "", false, true},
		{"/", "", false, true},
	} {
		got, changed, err := SafeFileName(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("%q: want refusal, got %q", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want || changed != c.changed {
			t.Errorf("%q: got %q changed=%v err=%v, want %q changed=%v", c.in, got, changed, err, c.want, c.changed)
		}
	}
	got, _, err := SafeFileName(long)
	if err != nil || len(got) > maxFileNameBytes || !strings.HasSuffix(got, ".txt") {
		t.Errorf("long name: len=%d err=%v", len(got), err)
	}
	composed, _, _ := SafeFileName("é")
	decomposed, _, _ := SafeFileName("e\u0301")
	if composed == decomposed {
		t.Error("composed and decomposed names must stay different names")
	}
}

func TestDispositionFilenameCannotEndTheHeaderOrStartAnother(t *testing.T) {
	for _, name := range []string{"a\r\nSet-Cookie: x=1", `a"; filename="b`, "naïve;name.txt", "100%.txt"} {
		h := DispositionFilename(name)
		if strings.ContainsAny(h, "\r\n") {
			t.Errorf("%q produced a header with a line break: %q", name, h)
		}
		first := strings.SplitN(h, "; filename*=", 2)[0]
		if strings.Count(first, `"`) != 2 {
			t.Errorf("%q: the quoted fallback is not one quoted string: %q", name, first)
		}
	}
	if got := DispositionFilename("naïve.txt"); !strings.Contains(got, "filename*=UTF-8''na%C3%AFve.txt") {
		t.Errorf("non-ascii name must keep its exact form in the encoded parameter: %q", got)
	}
}

func TestPolicyRefusesFromTheDescriptionAloneAndAllowsNothingByDefault(t *testing.T) {
	ok := FilePolicy{AllowUpload: true, AllowDownload: true, MaxBytes: 100, MediaTypes: []string{"text/*", "application/pdf"}, KeepName: true}
	good := FileSpec{Direction: "upload", Name: "a.txt", Size: 10, MediaType: "text/plain; charset=utf-8"}
	if err := ok.Check(good); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		why  string
		p    FilePolicy
		edit func(*FileSpec)
	}{
		{FileDirectionDenied, FilePolicy{}, func(*FileSpec) {}},
		{FileDirectionDenied, FilePolicy{AllowDownload: true, MediaTypes: ok.MediaTypes}, func(*FileSpec) {}},
		{FileTooLarge, ok, func(s *FileSpec) { s.Size = 101 }},
		{FileTypeDenied, ok, func(s *FileSpec) { s.MediaType = "application/x-msdownload" }},
		{FileTypeDenied, ok, func(s *FileSpec) { s.MediaType = "" }},
		{FileNameUnsafe, ok, func(s *FileSpec) { s.Name = "../a.txt" }},
		{FileNameUnsafe, ok, func(s *FileSpec) { s.Name = ".." }},
		{FileSpecInvalid, ok, func(s *FileSpec) { s.Direction = "sideload" }},
		{FileSpecInvalid, ok, func(s *FileSpec) { s.SHA256 = "ABC" }},
	} {
		s := good
		c.edit(&s)
		err := c.p.Check(s)
		if reasonOf(err) != c.why {
			t.Errorf("%+v: got %v, want %s", s, err, c.why)
		}
	}
}

func TestSandboxFilesBelongToOneSessionAndStayInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	sb, err := NewFileSandbox(root)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := sb.Stage("sess-a", "../../escape.txt", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sb.Open("sess-b", ref); reasonOf(err) != FileSessionMismatch {
		t.Errorf("another session opened the file: %v", err)
	}
	if err := sb.Drop("sess-b", ref); reasonOf(err) != FileSessionMismatch {
		t.Errorf("another session dropped the file: %v", err)
	}
	p, err := sb.Path("sess-a", ref)
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(root, p); err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("staged outside the root: %s", p)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("staged file permissions %v", st.Mode().Perm())
	}
	if err := os.WriteFile(p, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sb.Open("sess-a", ref); reasonOf(err) != FileIntegrity {
		t.Errorf("a changed staged file was served: %v", err)
	}
	if _, err := sb.Stage("bad/session\n", "a.txt", nil); reasonOf(err) != FileSessionMismatch {
		t.Errorf("session id with control characters accepted: %v", err)
	}
}

// fileSource is a documented file API with switches for the ways real sources misbehave.
type fileSource struct {
	mu        sync.Mutex
	files     map[string][]byte
	requests  int
	throttle  bool
	corrupt   bool // acknowledges an upload but keeps different bytes
	dropAfter bool // closes the connection after reading the upload
	short     bool // a download body shorter than its declared length
	srv       *httptest.Server
}

func newFileSource(t *testing.T) *fileSource {
	f := &fileSource{files: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fileSource) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests++
	f.mu.Unlock()
	if r.URL.Path == "/rec/r1/files" {
		f.mu.Lock()
		defer f.mu.Unlock()
		out := `{"version":"v1"`
		for n, b := range f.files {
			out += fmt.Sprintf(`,%q:%q`, "file:"+n, Digest(b)+":"+fmt.Sprint(len(b)))
		}
		_, _ = io.WriteString(w, out+"}")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/rec/r1/files/")
	switch r.Method {
	case http.MethodPut:
		if f.throttle {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if f.dropAfter {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		if got := r.Header.Get("X-Content-SHA256"); got != Digest(body) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if f.corrupt {
			body = append([]byte("X"), body...)
		}
		f.mu.Lock()
		f.files[name] = body
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet:
		f.mu.Lock()
		b, ok := f.files[name]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Disposition", DispositionFilename(name))
		if f.short {
			w.Header().Set("Content-Length", fmt.Sprint(len(b)))
			_, _ = w.Write(b[:len(b)/2])
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		_, _ = w.Write(b)
	}
}

func (f *fileSource) seen() int { f.mu.Lock(); defer f.mu.Unlock(); return f.requests }

func rigFiles(t *testing.T) (*FileExecutor, *FileSandbox, *fileSource) {
	src := newFileSource(t)
	sb, err := NewFileSandbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ex := NewFileExecutor(FileAPIConfig{
		BaseURL: src.srv.URL, ListPath: "/rec/%s/files", FilePath: "/rec/%s/files/%s", Dedup: true, Sandbox: sb,
		Policy: FilePolicy{AllowUpload: true, AllowDownload: true, MaxBytes: 1 << 20, MediaTypes: []string{"text/plain"}},
	})
	return ex, sb, src
}

func uploadCall(sb *FileSandbox, session, name string, content []byte, edit func(map[string]any)) Call {
	ref, err := sb.Stage(session, name, content)
	if err != nil {
		panic(err)
	}
	p := map[string]any{"direction": "upload", "name": name, "size": len(content), "sha256": Digest(content),
		"media_type": "text/plain", "session": session, "content_ref": ref}
	if edit != nil {
		edit(p)
	}
	return Call{Key: "k1", Target: Target{Record: "r1", Params: p}}
}

func TestUploadCommitsAndTheListingShowsTheSameDigestAndSize(t *testing.T) {
	ex, sb, _ := rigFiles(t)
	content := []byte("quarterly numbers\n")
	res, err := ex.Apply(context.Background(), uploadCall(sb, "s1", "Q3 résumé (final).txt", content, nil))
	if err != nil || res.Outcome != OutcomeCommitted {
		t.Fatalf("%+v %v", res, err)
	}
	snap, err := ex.Read(context.Background(), Target{Record: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"file:Q3 résumé (final).txt": Digest(content) + ":" + fmt.Sprint(len(content))}
	if !matches(snap.Fields, want) {
		t.Fatalf("listing %v does not show %v", snap.Fields, want)
	}
}

func TestUploadsAreRefusedBeforeAnythingReachesTheSource(t *testing.T) {
	ex, sb, src := rigFiles(t)
	content := []byte("payload")
	other, _ := sb.Stage("s2", "x.txt", []byte("other session"))
	for _, c := range []struct {
		why  string
		call Call
	}{
		{FileIntegrity, uploadCall(sb, "s1", "a.txt", content, func(p map[string]any) { p["sha256"] = Digest([]byte("different")) })},
		{FileIntegrity, uploadCall(sb, "s1", "a.txt", content, func(p map[string]any) { p["size"] = 3 })},
		{FileSessionMismatch, uploadCall(sb, "s1", "a.txt", content, func(p map[string]any) { p["content_ref"] = other })},
		{FileSessionMismatch, uploadCall(sb, "s1", "a.txt", content, func(p map[string]any) { p["session"] = "s2" })},
		{FileTypeDenied, uploadCall(sb, "s1", "a.exe", content, func(p map[string]any) { p["media_type"] = "application/x-msdownload" })},
		{FileTooLarge, uploadCall(sb, "s1", "big.txt", content, func(p map[string]any) { p["size"] = 2 << 20 })},
		{FileNameUnsafe, uploadCall(sb, "s1", "a.txt", content, func(p map[string]any) { p["name"] = ".." })},
		{FileContentMissing, uploadCall(sb, "s1", "a.txt", content, func(p map[string]any) { p["content_ref"] = "nope" })},
		{FileDirectionDenied, uploadCall(sb, "s1", "a.txt", content, func(p map[string]any) { p["direction"] = "download" })},
	} {
		if c.why == FileDirectionDenied {
			ex.cfg.Policy.AllowDownload = false
		}
		res, err := ex.Apply(context.Background(), c.call)
		if err != nil || res.Outcome != OutcomeRejected || res.Detail != c.why {
			t.Errorf("%s: %+v %v", c.why, res, err)
		}
	}
	if src.seen() != 0 {
		t.Fatalf("%d requests reached the source for refused uploads", src.seen())
	}
}

func TestAnAcknowledgedUploadWithDifferentStoredBytesDoesNotVerify(t *testing.T) {
	ex, sb, src := rigFiles(t)
	src.corrupt = true
	content := []byte("exact bytes")
	res, _ := ex.Apply(context.Background(), uploadCall(sb, "s1", "a.txt", content, nil))
	if res.Outcome != OutcomeCommitted {
		t.Fatalf("%+v", res)
	}
	snap, _ := ex.Read(context.Background(), Target{Record: "r1"})
	if matches(snap.Fields, res.Echo) {
		t.Fatal("the read-back digest matched although the source kept other bytes")
	}
}

func TestConnectionLostAfterSendingIsUnknownAndNeverNotSent(t *testing.T) {
	ex, sb, src := rigFiles(t)
	src.dropAfter = true
	_, err := ex.Apply(context.Background(), uploadCall(sb, "s1", "a.txt", []byte("x"), nil))
	if err == nil || errors.Is(err, ErrNotSent) {
		t.Fatalf("a lost response after sending must be an unknown outcome, got %v", err)
	}
	if src.seen() != 1 {
		t.Fatalf("the upload was sent %d times", src.seen())
	}
}

func TestAThrottledUploadIsNoEffectAndCarriesTheHint(t *testing.T) {
	ex, sb, src := rigFiles(t)
	src.throttle = true
	res, err := ex.Apply(context.Background(), uploadCall(sb, "s1", "a.txt", []byte("x"), nil))
	if err != nil || res.Outcome != OutcomeThrottled || res.RetryAfter.Seconds() != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if len(src.files) != 0 {
		t.Fatal("a throttled upload stored a file")
	}
}

func TestDownloadStagesVerifiedBytesUnderASafeNameForOneSession(t *testing.T) {
	ex, sb, src := rigFiles(t)
	content := []byte("export,rows\n1,2\n")
	src.files["../../report 1.csv"] = content
	call := Call{Target: Target{Record: "r1", Params: map[string]any{
		"direction": "export", "name": "../../report 1.csv", "size": len(content), "sha256": Digest(content),
		"media_type": "text/plain", "session": "s1",
	}}}
	res, err := ex.Apply(context.Background(), call)
	if err != nil || res.Outcome != OutcomeCommitted {
		t.Fatalf("%+v %v", res, err)
	}
	if res.Echo["name"] != "report 1.csv" || res.Echo["sha256"] != Digest(content) {
		t.Fatalf("%v", res.Echo)
	}
	got, _, err := sb.Open("s1", res.Echo["content_ref"])
	if err != nil || string(got) != string(content) {
		t.Fatalf("%q %v", got, err)
	}
	if _, _, err := sb.Open("s2", res.Echo["content_ref"]); reasonOf(err) != FileSessionMismatch {
		t.Fatalf("another session read the download: %v", err)
	}
}

func TestDownloadsFailClosedOnWrongBytesOversizeAndInterruption(t *testing.T) {
	ex, sb, src := rigFiles(t)
	content := []byte("0123456789")
	src.files["a.txt"] = content
	spec := func(edit func(map[string]any)) Call {
		p := map[string]any{"direction": "download", "name": "a.txt", "size": len(content), "sha256": Digest(content), "media_type": "text/plain", "session": "s1"}
		if edit != nil {
			edit(p)
		}
		return Call{Target: Target{Record: "r1", Params: p}}
	}
	if res, _ := ex.Apply(context.Background(), spec(func(p map[string]any) { p["sha256"] = Digest([]byte("other")) })); res.Outcome != OutcomeRejected || res.Detail != FileIntegrity {
		t.Errorf("digest mismatch: %+v", res)
	}
	ex.cfg.Policy.MaxBytes = 4
	if res, _ := ex.Apply(context.Background(), spec(func(p map[string]any) { p["size"] = 3 })); res.Outcome != OutcomeRejected || res.Detail != FileTooLarge {
		t.Errorf("oversize: %+v", res)
	}
	ex.cfg.Policy.MaxBytes = 1 << 20
	src.short = true
	if res, _ := ex.Apply(context.Background(), spec(nil)); res.Outcome != OutcomeUnknown {
		t.Errorf("interrupted download: %+v", res)
	}
	entries, _ := os.ReadDir(sb.root)
	for _, e := range entries {
		sub, _ := os.ReadDir(filepath.Join(sb.root, e.Name()))
		if len(sub) != 0 {
			t.Errorf("a failed download left %d staged file(s)", len(sub))
		}
	}
}

func TestTheDeclaredNameComesFromTheHeaderNotTheFileSystem(t *testing.T) {
	for _, c := range []struct{ header, want string }{
		{DispositionFilename("Zusammenfassung ü.csv"), "Zusammenfassung ü.csv"},
		{DispositionFilename(`a "b" ; c.txt`), `a _b_ ; c.txt`},
		{`attachment; filename="../../x.txt"`, "x.txt"},
	} {
		got, _, err := NameFromDisposition(c.header)
		if err != nil || got != c.want {
			t.Errorf("%q: got %q %v, want %q", c.header, got, err, c.want)
		}
	}
	for _, bad := range []string{"inline", "attachment", `attachment; filename=".."`, "\x00"} {
		if _, _, err := NameFromDisposition(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
