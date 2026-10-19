package execution

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

var devtoolsLine = regexp.MustCompile(`DevTools listening on (ws://\S+)`)

type cdpReply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Browser is a headless Chromium driven over the DevTools protocol.
type Browser struct {
	cmd     *exec.Cmd
	ws      *wsConn
	dir     string
	mu      sync.Mutex
	next    int
	waiting map[int]chan cdpReply
	closed  chan struct{}
}

type BrowserOptions struct {
	Path      string
	NoSandbox bool
}

func LaunchBrowser(ctx context.Context, opt BrowserOptions) (*Browser, error) {
	dir, err := os.MkdirTemp("", "vsf-browser-")
	if err != nil {
		return nil, err
	}
	args := []string{"--headless=new", "--remote-debugging-port=0", "--user-data-dir=" + dir, "--no-first-run",
		"--no-default-browser-check", "--disable-extensions", "--disable-gpu", "about:blank"}
	if opt.NoSandbox {
		args = append(args, "--no-sandbox")
	}
	cmd := exec.Command(opt.Path, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if m := devtoolsLine.FindStringSubmatch(sc.Text()); m != nil {
				found <- m[1]
				break
			}
		}
		for sc.Scan() {
		}
	}()
	var endpoint string
	select {
	case endpoint = <-found:
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		_ = os.RemoveAll(dir)
		return nil, errors.New("browser did not report a DevTools endpoint")
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = os.RemoveAll(dir)
		return nil, ctx.Err()
	}
	ws, err := wsDial(ctx, endpoint)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	b := &Browser{cmd: cmd, ws: ws, dir: dir, waiting: map[int]chan cdpReply{}, closed: make(chan struct{})}
	go b.readLoop()
	return b, nil
}

func (b *Browser) readLoop() {
	defer close(b.closed)
	for {
		raw, err := b.ws.ReadMessage()
		if err != nil {
			b.mu.Lock()
			for id, ch := range b.waiting {
				ch <- cdpReply{Error: &struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}{Message: "browser connection closed"}}
				delete(b.waiting, id)
			}
			b.mu.Unlock()
			return
		}
		var m struct {
			ID int `json:"id"`
			cdpReply
		}
		if json.Unmarshal(raw, &m) != nil || m.ID == 0 {
			continue
		}
		b.mu.Lock()
		ch := b.waiting[m.ID]
		delete(b.waiting, m.ID)
		b.mu.Unlock()
		if ch != nil {
			ch <- m.cdpReply
		}
	}
}

func (b *Browser) call(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	b.mu.Lock()
	b.next++
	id := b.next
	ch := make(chan cdpReply, 1)
	b.waiting[id] = ch
	b.mu.Unlock()
	req := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		req["sessionId"] = session
	}
	raw, _ := json.Marshal(req)
	if err := b.ws.WriteText(raw); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, r.Error.Message)
		}
		return r.Result, nil
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (b *Browser) Close() error {
	_ = b.ws.Close()
	_ = b.cmd.Process.Kill()
	_ = b.cmd.Wait()
	return os.RemoveAll(b.dir)
}

// Page is one tab.
type Page struct {
	b       *Browser
	target  string
	session string
}

func (b *Browser) NewPage(ctx context.Context) (*Page, error) {
	raw, err := b.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		return nil, err
	}
	var t struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	raw, err = b.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": t.TargetID, "flatten": true})
	if err != nil {
		return nil, err
	}
	var s struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	p := &Page{b: b, target: t.TargetID, session: s.SessionID}
	if _, err := b.call(ctx, p.session, "Page.enable", map[string]any{}); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Page) Close(ctx context.Context) {
	_, _ = p.b.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": p.target})
}

// Navigate loads a url and waits for the document to finish loading.
func (p *Page) Navigate(ctx context.Context, target string) error {
	raw, err := p.b.call(ctx, p.session, "Page.navigate", map[string]any{"url": target})
	if err != nil {
		return err
	}
	var n struct {
		ErrorText string `json:"errorText"`
	}
	_ = json.Unmarshal(raw, &n)
	if n.ErrorText != "" {
		return fmt.Errorf("%w: %s", ErrNotSent, n.ErrorText)
	}
	for {
		var ready string
		if err := p.Eval(ctx, `document.readyState`, &ready); err == nil && ready == "complete" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Eval runs a JavaScript expression in the page and decodes its JSON-serializable value.
func (p *Page) Eval(ctx context.Context, expr string, out any) error {
	raw, err := p.b.call(ctx, p.session, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true})
	if err != nil {
		return err
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return err
	}
	if r.Exception != nil {
		return fmt.Errorf("page script failed: %s", r.Exception.Text)
	}
	if out == nil || len(r.Result.Value) == 0 {
		return nil
	}
	return json.Unmarshal(r.Result.Value, out)
}
