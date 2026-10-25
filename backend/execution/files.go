package execution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Refusal reasons for file transfers; each is decided before anything reaches the source.
const (
	FileNameUnsafe       = "file.name_unsafe"
	FileTooLarge         = "file.too_large"
	FileTypeDenied       = "file.type_denied"
	FileDirectionDenied  = "file.direction_denied"
	FileSessionMismatch  = "file.session_mismatch"
	FileIntegrity        = "file.integrity"
	FileContentMissing   = "file.content_missing"
	FileSpecInvalid      = "file.spec_invalid"
	maxFileNameBytes     = 255
	maxStagedFilesPerRun = 64
)

var windowsReserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// SafeFileName turns a name a page or a person supplied into one that is safe to store, print and send.
// It never silently drops the meaning of a name: anything it must change is reported as changed, and names
// that cannot be made safe (empty, dots only, path escape that leaves nothing) are refused.
func SafeFileName(raw string) (name string, changed bool, err error) {
	if !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
		return "", false, errors.New(FileNameUnsafe)
	}
	s := raw // names stay byte-for-byte: composed and decomposed forms are different names, never silently merged
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsControl(r), r == '\u2028', r == '\u2029', r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069', r == '\ufeff':
			b.WriteRune('_')
		case strings.ContainsRune(`<>:"|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	s = strings.TrimRight(b.String(), " .")
	s = strings.TrimLeft(s, " ")
	if s == "" || strings.Trim(s, ".") == "" || strings.Trim(s, "_") == "" {
		return "", false, errors.New(FileNameUnsafe)
	}
	stem := strings.ToLower(strings.SplitN(s, ".", 2)[0])
	if windowsReserved[stem] {
		s = "_" + s
	}
	for len(s) > maxFileNameBytes {
		ext := filepath.Ext(s)
		if len(ext) > 16 {
			ext = ""
		}
		stem := strings.TrimSuffix(s, ext)
		_, size := utf8.DecodeLastRuneInString(stem)
		s = stem[:len(stem)-size] + ext
	}
	return s, s != raw, nil
}

// DispositionFilename renders a name for a Content-Disposition header: an ASCII fallback and the RFC 5987 form,
// with nothing that could end the header value or start another header.
func DispositionFilename(name string) string {
	var ascii strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r > 0x7e || r == '"' || r == '\\' || r == ';' || r == '%' {
			ascii.WriteByte('_')
		} else {
			ascii.WriteRune(r)
		}
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii.String(), url.PathEscape(name))
}

// NameFromDisposition reads the name a source declared for a download and makes it safe; the file system's name for
// the saved file is never trusted, because a browser may replace what it cannot store.
func NameFromDisposition(header string) (string, bool, error) {
	_, params, err := mime.ParseMediaType(header)
	if err != nil || params["filename"] == "" {
		return "", false, errors.New(FileNameUnsafe)
	}
	return SafeFileName(params["filename"])
}

// FileSpec describes one transfer before it happens.
type FileSpec struct {
	Direction string // "upload", "download" or "export"
	Name      string
	Size      int64
	SHA256    string
	MediaType string
}

// FilePolicy is what a session is allowed to move; the zero value allows nothing.
type FilePolicy struct {
	AllowUpload   bool
	AllowDownload bool
	MaxBytes      int64
	// MediaTypes lists allowed types, exact or as "type/*"; empty means no type is allowed.
	MediaTypes []string
	// KeepName refuses a name that SafeFileName would have to change.
	KeepName bool
}

// Check decides a transfer from its description alone, with the reason a person or log can read.
func (p FilePolicy) Check(s FileSpec) error {
	switch s.Direction {
	case "upload":
		if !p.AllowUpload {
			return refuse(FileDirectionDenied)
		}
	case "download", "export":
		if !p.AllowDownload {
			return refuse(FileDirectionDenied)
		}
	default:
		return refuse(FileSpecInvalid)
	}
	safe, changed, err := SafeFileName(s.Name)
	if err != nil || safe == "" || (p.KeepName && changed) {
		return refuse(FileNameUnsafe)
	}
	if s.Size < 0 || (s.SHA256 != "" && !validDigest(s.SHA256)) {
		return refuse(FileSpecInvalid)
	}
	if p.MaxBytes > 0 && s.Size > p.MaxBytes {
		return refuse(FileTooLarge)
	}
	if !p.allowsType(s.MediaType) {
		return refuse(FileTypeDenied)
	}
	return nil
}

func (p FilePolicy) allowsType(t string) bool {
	mt, _, err := mime.ParseMediaType(t)
	if err != nil {
		return false
	}
	for _, a := range p.MediaTypes {
		if a == mt || (strings.HasSuffix(a, "/*") && strings.HasPrefix(mt, strings.TrimSuffix(a, "*"))) {
			return true
		}
	}
	return false
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && s == strings.ToLower(s)
}

// Digest is the sha256 of bytes in the lowercase hex form specs use.
func Digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// FileSandbox holds the bytes of files in flight, keyed by session, on disk with private permissions.
// A file staged for one session cannot be read, sent or deleted by another.
type FileSandbox struct {
	mu   sync.Mutex
	root string
	meta map[string]stagedFile
}

type stagedFile struct {
	session, path, digest string
	size                  int64
}

func NewFileSandbox(root string) (*FileSandbox, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &FileSandbox{root: root, meta: map[string]stagedFile{}}, nil
}

func (s *FileSandbox) dir(session string) (string, error) {
	if !validID(session, 128) {
		return "", refuse(FileSessionMismatch)
	}
	d := filepath.Join(s.root, Digest([]byte(session))[:32])
	return d, os.MkdirAll(d, 0o700)
}

// Stage stores content under a safe name for one session and returns the reference to give an operation.
func (s *FileSandbox) Stage(session, name string, content []byte) (ref string, err error) {
	safe, _, err := SafeFileName(name)
	if err != nil {
		return "", refuse(FileNameUnsafe)
	}
	d, err := s.dir(session)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, f := range s.meta {
		if f.session == session {
			n++
		}
	}
	if n >= maxStagedFilesPerRun {
		return "", refuse(FileTooLarge)
	}
	sum := Digest(content)
	ref = sum[:16] + "-" + Digest([]byte(session + "\x00" + safe))[:16]
	path := filepath.Join(d, ref)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return "", err
	}
	s.meta[ref] = stagedFile{session: session, path: path, digest: sum, size: int64(len(content))}
	return ref, nil
}

// Open returns the staged bytes only to the session that staged them.
func (s *FileSandbox) Open(session, ref string) ([]byte, string, error) {
	s.mu.Lock()
	f, ok := s.meta[ref]
	s.mu.Unlock()
	if !ok {
		return nil, "", refuse(FileContentMissing)
	}
	if f.session != session {
		return nil, "", refuse(FileSessionMismatch)
	}
	b, err := os.ReadFile(f.path)
	if err != nil {
		return nil, "", refuse(FileContentMissing)
	}
	if Digest(b) != f.digest {
		return nil, "", refuse(FileIntegrity)
	}
	return b, f.digest, nil
}

// Path is where a staged file lives, for a browser that must be handed a path; it checks the session first.
func (s *FileSandbox) Path(session, ref string) (string, error) {
	if _, _, err := s.Open(session, ref); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta[ref].path, nil
}

// Drop removes one staged file of the session; other sessions' files are never touched.
func (s *FileSandbox) Drop(session, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.meta[ref]
	if !ok || f.session != session {
		return refuse(FileSessionMismatch)
	}
	delete(s.meta, ref)
	return os.Remove(f.path)
}

// FileAPIConfig describes a documented HTTP file API: where files are listed, sent and fetched.
type FileAPIConfig struct {
	BaseURL string
	// ListPath and FilePath take the record id; FilePath also takes the file name.
	ListPath string
	FilePath string
	Client   *http.Client
	Dedup    bool
	Policy   FilePolicy
	Sandbox  *FileSandbox
}

// FileExecutor moves a file between a session's sandbox and a source, and says only what the source showed.
type FileExecutor struct{ cfg FileAPIConfig }

func NewFileExecutor(cfg FileAPIConfig) *FileExecutor {
	c := http.Client{}
	if cfg.Client != nil {
		c = *cfg.Client
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cfg.Client = &c
	return &FileExecutor{cfg: cfg}
}

func (e *FileExecutor) Kind() string { return "provider_file_api" }

func (e *FileExecutor) Capabilities() Capabilities {
	return Capabilities{DedupTokens: e.cfg.Dedup, ReadBack: true, AuthoritativeStatus: true}
}

func (e *FileExecutor) listURL(record string) string {
	return e.cfg.BaseURL + fmt.Sprintf(e.cfg.ListPath, url.PathEscape(record))
}

func (e *FileExecutor) fileURL(record, name string) string {
	return e.cfg.BaseURL + fmt.Sprintf(e.cfg.FilePath, url.PathEscape(record), url.PathEscape(name))
}

// Read lists the record's files as "file:<name>" = "<sha256>:<size>", which is what an upload is verified against.
func (e *FileExecutor) Read(ctx context.Context, t Target) (Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.listURL(t.Record), nil)
	if err != nil {
		return Snapshot{}, err
	}
	res, err := e.cfg.Client.Do(req)
	if err != nil {
		return Snapshot{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("list: status %d", res.StatusCode)
	}
	return parseFileListing(io.LimitReader(res.Body, 1<<20))
}

func parseFileListing(r io.Reader) (Snapshot, error) {
	var doc map[string]any
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Fields: map[string]string{}}
	for k, v := range doc {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if k == "version" {
			snap.Version = s
			continue
		}
		snap.Fields[k] = s
	}
	return snap, nil
}

// Apply performs one transfer. Policy, session binding and integrity are decided before the source is contacted.
func (e *FileExecutor) Apply(ctx context.Context, c Call) (Result, error) {
	spec, session, ref, err := fileCall(c.Target.Params)
	if err != nil {
		return Result{Outcome: OutcomeRejected, Detail: err.Error()}, nil
	}
	if err := e.cfg.Policy.Check(spec); err != nil {
		return Result{Outcome: OutcomeRejected, Detail: reasonOf(err)}, nil
	}
	safe, _, _ := SafeFileName(spec.Name)
	switch spec.Direction {
	case "upload":
		return e.upload(ctx, c, spec, safe, session, ref)
	default:
		return e.download(ctx, c, spec, safe, session)
	}
}

func fileCall(p map[string]any) (FileSpec, string, string, error) {
	str := func(k string) string { s, _ := p[k].(string); return s }
	size, _ := p["size"].(float64)
	if n, ok := p["size"].(int64); ok {
		size = float64(n)
	}
	if n, ok := p["size"].(int); ok {
		size = float64(n)
	}
	s := FileSpec{Direction: str("direction"), Name: str("name"), Size: int64(size), SHA256: str("sha256"), MediaType: str("media_type")}
	if s.Direction == "" || s.Name == "" {
		return s, "", "", errors.New(FileSpecInvalid)
	}
	return s, str("session"), str("content_ref"), nil
}

func (e *FileExecutor) upload(ctx context.Context, c Call, spec FileSpec, safe, session, ref string) (Result, error) {
	if e.cfg.Sandbox == nil {
		return Result{Outcome: OutcomeRejected, Detail: FileContentMissing}, nil
	}
	body, digest, err := e.cfg.Sandbox.Open(session, ref)
	if err != nil {
		return Result{Outcome: OutcomeRejected, Detail: reasonOf(err)}, nil
	}
	if int64(len(body)) != spec.Size || (spec.SHA256 != "" && digest != spec.SHA256) {
		return Result{Outcome: OutcomeRejected, Detail: FileIntegrity}, nil
	}
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, ctx.Err())
	}
	Progress(ctx, 0.1, "sending "+safe)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, e.fileURL(c.Target.Record, safe), bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", spec.MediaType)
	req.Header.Set("X-Content-SHA256", digest)
	if c.IfMatch != "" {
		req.Header.Set("If-Match", `"`+c.IfMatch+`"`)
	}
	if c.DedupToken != "" && e.cfg.Dedup {
		req.Header.Set("Idempotency-Key", c.DedupToken)
	}
	res, err := e.cfg.Client.Do(req)
	if err != nil {
		if notSent(err) {
			return Result{}, fmt.Errorf("%w: %v", ErrNotSent, err)
		}
		return Result{}, err
	}
	defer res.Body.Close()
	Progress(ctx, 0.9, "sent "+safe)
	out := classifyStatus(res.StatusCode, res.Header.Get("Retry-After"), res.Body)
	if out.Outcome == OutcomeCommitted {
		out.Echo = map[string]string{"file:" + safe: digest + ":" + fmt.Sprint(len(body))}
	}
	return out, nil
}

func (e *FileExecutor) download(ctx context.Context, c Call, spec FileSpec, safe, session string) (Result, error) {
	if e.cfg.Sandbox == nil {
		return Result{Outcome: OutcomeRejected, Detail: FileContentMissing}, nil
	}
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, ctx.Err())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.fileURL(c.Target.Record, spec.Name), nil)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	res, err := e.cfg.Client.Do(req)
	if err != nil {
		if notSent(err) {
			return Result{}, fmt.Errorf("%w: %v", ErrNotSent, err)
		}
		return Result{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return classifyStatus(res.StatusCode, res.Header.Get("Retry-After"), res.Body), nil
	}
	limit := e.cfg.Policy.MaxBytes
	if limit <= 0 {
		limit = 64 << 20
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return Result{Outcome: OutcomeUnknown, Detail: "download interrupted"}, nil
	}
	if int64(len(body)) > limit {
		return Result{Outcome: OutcomeRejected, Detail: FileTooLarge}, nil
	}
	sum := Digest(body)
	if (spec.SHA256 != "" && sum != spec.SHA256) || (spec.Size > 0 && int64(len(body)) != spec.Size) {
		return Result{Outcome: OutcomeRejected, Detail: FileIntegrity}, nil
	}
	if res.ContentLength >= 0 && res.ContentLength != int64(len(body)) {
		return Result{Outcome: OutcomeRejected, Detail: FileIntegrity}, nil
	}
	ref, err := e.cfg.Sandbox.Stage(session, safe, body)
	if err != nil {
		return Result{Outcome: OutcomeRejected, Detail: reasonOf(err)}, nil
	}
	Progress(ctx, 1, "received "+safe)
	return Result{Outcome: OutcomeCommitted, Echo: map[string]string{"content_ref": ref, "sha256": sum, "size": fmt.Sprint(len(body)), "name": safe}}, nil
}
