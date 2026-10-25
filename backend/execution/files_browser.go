package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SetInputFiles gives a file input staged files by path; it refuses non-file inputs and paths outside allowedDir.
func (p *Page) SetInputFiles(ctx context.Context, selector string, allowedDir string, paths []string) error {
	root, err := filepath.EvalSymlinks(allowedDir)
	if err != nil {
		return err
	}
	for _, f := range paths {
		real, err := filepath.EvalSymlinks(f)
		if err != nil {
			return err
		}
		if rel, err := filepath.Rel(root, real); err != nil || strings.HasPrefix(rel, "..") {
			return refuse(FileSessionMismatch)
		}
	}
	sel, _ := json.Marshal(selector)
	raw, err := p.b.call(ctx, p.session, "Runtime.evaluate", map[string]any{
		"expression": fmt.Sprintf(`(() => { const e = document.querySelector(%s); return e instanceof HTMLInputElement && e.type === "file" ? e : null; })()`, sel),
	})
	if err != nil {
		return err
	}
	var ev struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return err
	}
	if ev.Result.ObjectID == "" {
		return errors.New("no file input matches the selector")
	}
	if _, err := p.b.call(ctx, p.session, "DOM.enable", map[string]any{}); err != nil {
		return err
	}
	_, err = p.b.call(ctx, p.session, "DOM.setFileInputFiles", map[string]any{"objectId": ev.Result.ObjectID, "files": paths})
	return err
}

// AllowDownloads sends the browser's downloads to one directory, so a finished file can be found and verified.
func (b *Browser) AllowDownloads(ctx context.Context, dir string) error {
	_, err := b.call(ctx, "", "Browser.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": dir})
	return err
}

// AwaitDownload waits for one new, completed file in dir whose size has stopped changing.
func AwaitDownload(ctx context.Context, dir string, known map[string]bool, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var last string
	var lastSize int64 = -1
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", err
		}
		for _, e := range entries {
			n := e.Name()
			if known[n] || strings.HasSuffix(n, ".crdownload") || strings.HasPrefix(n, ".") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if n == last && info.Size() == lastSize {
				return filepath.Join(dir, n), nil
			}
			last, lastSize = n, info.Size()
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return "", errors.New("no download completed")
}

// ListDir names the files already in dir so a later download can be told apart from them.
func ListDir(dir string) map[string]bool {
	out := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}
