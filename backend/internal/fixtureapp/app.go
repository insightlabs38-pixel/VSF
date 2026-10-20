// Package fixtureapp is a controlled source application reachable as a documented API and as a web page.
package fixtureapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Order struct {
	ID                   string
	Stage                string
	Version              int
	ConfirmationRequired bool
}

type Faults struct {
	ThrottleNext      int
	DropAfterCommit   int
	DropBeforeCommit  int
	DelayBeforeCommit time.Duration
}

type App struct {
	mu        sync.Mutex
	orders    map[string]*Order
	seen      map[string]string
	receipts  map[string]string
	fenced    map[string]bool
	Receipts  bool
	Fence     bool
	Faults    Faults
	commits   int
	reqs      []string
	attempts  map[string]bool
	srv       *httptest.Server
	Dedup     bool
	KeepAlive bool
	stale     map[string]*Order
}

var stages = map[string]bool{"new": true, "qualified": true, "proposal": true, "won": true}

func New(dedup bool) *App {
	a := &App{orders: map[string]*Order{}, seen: map[string]string{}, receipts: map[string]string{}, fenced: map[string]bool{}, Dedup: dedup}
	a.Put(Order{ID: "o1", Stage: "new", Version: 1})
	a.Put(Order{ID: "o2", Stage: "new", Version: 1, ConfirmationRequired: true})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/orders/{id}", a.get)
	mux.HandleFunc("POST /api/orders/{id}/stage", a.post)
	mux.HandleFunc("GET /api/receipts/{key}", a.receipt)
	mux.HandleFunc("GET /ui/orders/{id}", a.page)
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		keep := a.KeepAlive
		a.mu.Unlock()
		if !keep {
			w.Header().Set("Connection", "close")
		}
		mux.ServeHTTP(w, r)
	}))
	return a
}

// SetReceipts makes the source report per key whether a request committed; with fence it also refuses unseen keys.
func (a *App) SetReceipts(on, fence bool) {
	a.mu.Lock()
	a.Receipts, a.Fence = on, fence
	a.mu.Unlock()
}

// SetStale makes reads of one order report an older snapshot, the way a lagging replica does.
func (a *App) SetStale(id string, snap *Order) {
	a.mu.Lock()
	if a.stale == nil {
		a.stale = map[string]*Order{}
	}
	if snap == nil {
		delete(a.stale, id)
	} else {
		a.stale[id] = snap
	}
	a.mu.Unlock()
}

func (a *App) receipt(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	a.mu.Lock()
	on, fence := a.Receipts, a.Fence
	rec, ok := a.receipts[key]
	if on && !ok && fence {
		a.fenced[key] = true
	}
	a.mu.Unlock()
	if !on {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if ok {
		_, _ = w.Write([]byte(rec))
		return
	}
	w.WriteHeader(http.StatusNotFound)
	_, _ = fmt.Fprintf(w, `{"key":%q,"committed":false,"fenced":%t}`, key, fence)
}

// SetKeepAlive lets clients reuse connections, the way real sources usually allow.
func (a *App) SetKeepAlive(on bool) {
	a.mu.Lock()
	a.KeepAlive = on
	a.mu.Unlock()
}

func (a *App) URL() string { return a.srv.URL }
func (a *App) Close()      { a.srv.Close() }

func (a *App) Put(o Order) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.orders[o.ID] = &o
}

func (a *App) Order(id string) Order {
	a.mu.Lock()
	defer a.mu.Unlock()
	if o := a.orders[id]; o != nil {
		return *o
	}
	return Order{}
}

func (a *App) Commits() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.commits
}

// attempt tells caller submissions apart from a browser re-sending one fetch after a dropped connection.
func (a *App) attempt(r *http.Request) {
	id := r.Header.Get("X-Submit")
	if id == "" {
		id = fmt.Sprintf("api-%d", len(a.reqs))
	}
	if a.attempts == nil {
		a.attempts = map[string]bool{}
	}
	a.attempts[id] = true
}

// WriteAttempts counts distinct caller submissions; transport-level re-sends of one fetch count once.
func (a *App) WriteAttempts() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.attempts)
}

func (a *App) Requests() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.reqs...)
}

func (a *App) SetFaults(f Faults) {
	a.mu.Lock()
	a.Faults = f
	a.mu.Unlock()
}

// Change edits an order the way another user would, advancing its version.
func (a *App) Change(id, stage string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	o := a.orders[id]
	o.Stage = stage
	o.Version++
}

func version(o *Order) string { return "v" + strconv.Itoa(o.Version) }

func (a *App) get(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	o := a.orders[r.PathValue("id")]
	a.reqs = append(a.reqs, "GET "+r.URL.Path)
	var out Order
	if o != nil {
		out = *o
		if st := a.stale[o.ID]; st != nil {
			out = *st
		}
	}
	a.mu.Unlock()
	if o == nil {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("ETag", `"v`+strconv.Itoa(out.Version)+`"`)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": out.ID, "stage": out.Stage, "version": "v" + strconv.Itoa(out.Version), "confirmation_required": out.ConfirmationRequired,
	})
}

func (a *App) post(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Stage string `json:"stage"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	key := r.Header.Get("Idempotency-Key")
	a.mu.Lock()
	a.reqs = append(a.reqs, "POST "+r.URL.Path)
	a.attempt(r)
	f := a.Faults
	o := a.orders[r.PathValue("id")]
	if key != "" && a.fenced[key] {
		a.mu.Unlock()
		http.Error(w, `{"error":"fenced"}`, http.StatusConflict)
		return
	}
	if a.Dedup && key != "" {
		if prior, ok := a.seen[key]; ok {
			a.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(prior))
			return
		}
	}
	if f.ThrottleNext > 0 {
		a.Faults.ThrottleNext--
		a.mu.Unlock()
		w.Header().Set("Retry-After", "1")
		http.Error(w, `{"error":"throttled"}`, http.StatusTooManyRequests)
		return
	}
	a.mu.Unlock()
	if f.DelayBeforeCommit > 0 {
		time.Sleep(f.DelayBeforeCommit)
	}
	if o == nil {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}
	if !stages[body.Stage] {
		http.Error(w, `{"error":"invalid_stage"}`, http.StatusUnprocessableEntity)
		return
	}
	a.mu.Lock()
	if key != "" && a.fenced[key] {
		a.mu.Unlock()
		http.Error(w, `{"error":"fenced"}`, http.StatusConflict)
		return
	}
	if m := strings.Trim(r.Header.Get("If-Match"), `"`); m != "" && m != version(o) {
		a.mu.Unlock()
		http.Error(w, `{"error":"version_conflict"}`, http.StatusConflict)
		return
	}
	if f.DropBeforeCommit > 0 {
		a.Faults.DropBeforeCommit--
		a.mu.Unlock()
		drop(w)
		return
	}
	o.Stage = body.Stage
	o.Version++
	a.commits++
	resp := fmt.Sprintf(`{"id":%q,"stage":%q,"version":%q}`, o.ID, o.Stage, version(o))
	if key != "" {
		a.seen[key] = resp
		a.receipts[key] = fmt.Sprintf(`{"key":%q,"record":%q,"committed":true,"version":%q}`, key, o.ID, version(o))
	}
	if f.DropAfterCommit > 0 {
		a.Faults.DropAfterCommit--
		a.mu.Unlock()
		drop(w)
		return
	}
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(resp))
}

func drop(w http.ResponseWriter) {
	if h, ok := w.(http.Hijacker); ok {
		if c, _, err := h.Hijack(); err == nil {
			_ = c.Close()
		}
	}
}

const pageHTML = `<!doctype html><meta charset="utf-8"><title>Order</title>
<main><h1>Order <span id="order-id">%s</span></h1>
<p>Stage: <strong id="stage" data-version="%s">%s</strong></p>
<label for="next">New stage</label>
<select id="next"><option>new</option><option>qualified</option><option>proposal</option><option>won</option><option>bogus</option></select>
<button id="save">Save</button>
<p id="result" role="status" data-status=""></p></main>
<script>
const id = document.getElementById("order-id").textContent;
let submits = 0;
document.getElementById("save").addEventListener("click", async () => {
  const stage = document.getElementById("stage");
  const out = document.getElementById("result");
  out.dataset.status = "pending";
  try {
    const res = await fetch("/api/orders/" + id + "/stage", {
      method: "POST",
      headers: { "content-type": "application/json", "if-match": stage.dataset.version, "x-submit": String(++submits) },
      body: JSON.stringify({ stage: document.getElementById("next").value }),
    });
    const text = await res.text();
    const map = { 200: "committed", 409: "conflict", 422: "rejected", 404: "rejected", 429: "throttled" };
    if (res.status === 200) {
      const d = JSON.parse(text);
      stage.textContent = d.stage; stage.dataset.version = d.version;
    }
    out.dataset.status = map[res.status] || "unknown";
    out.dataset.retryAfter = res.headers.get("retry-after") || "";
    out.textContent = out.dataset.status;
  } catch (e) {
    out.dataset.status = "network-error";
    out.textContent = "network error";
  }
});
</script>`

func (a *App) page(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	o := a.orders[r.PathValue("id")]
	a.reqs = append(a.reqs, "GET "+r.URL.Path)
	var out Order
	if o != nil {
		out = *o
	}
	a.mu.Unlock()
	if o == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, pageHTML, out.ID, "v"+strconv.Itoa(out.Version), out.Stage)
}
