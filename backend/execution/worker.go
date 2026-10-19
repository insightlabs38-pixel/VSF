package execution

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/ir"
)

// Env is what the host knows about the moment and place of execution.
type Env struct {
	Now              int64  `json:"now"`
	RuntimeID        string `json:"runtime_id"`
	SourceState      string `json:"source_state"`
	CapabilityDigest string `json:"capability_digest"`
	PrincipalRef     string `json:"principal_ref"`
	VariantID        string `json:"variant_id"`
}

// Authorized exists only because the Rust validator accepted this exact document in this exact environment.
type Authorized struct {
	digest   string
	contract ir.OperationContract
	env      Env
	minted   bool
}

func (a Authorized) Digest() string                 { return a.digest }
func (a Authorized) Contract() ir.OperationContract { return a.contract }
func (a Authorized) Env() Env                       { return a.env }
func (a Authorized) valid() bool                    { return a.minted && a.digest != "" }

// Authorizer is how execution obtains Authorized values; the Rust worker is the only production implementation.
type Authorizer interface {
	Authorize(doc []byte, env Env) (Authorized, error)
}

const maxFrame = 8 << 20

// Worker talks to the native validator over length-prefixed stdio frames.
type Worker struct {
	mu  sync.Mutex
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func StartWorker(path string) (*Worker, error) {
	cmd := exec.Command(path, "serve")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Worker{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

func (w *Worker) Close() error {
	_ = w.in.Close()
	return w.cmd.Wait()
}

func (w *Worker) call(op byte, payload []byte) ([]byte, error) {
	if len(payload)+1 > maxFrame {
		return nil, errors.New("frame too large")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	head := make([]byte, 5)
	binary.LittleEndian.PutUint32(head, uint32(len(payload)+1))
	head[4] = op
	if _, err := w.in.Write(append(head, payload...)); err != nil {
		return nil, err
	}
	var status [1]byte
	if _, err := io.ReadFull(w.out, status[:]); err != nil {
		return nil, err
	}
	var n [4]byte
	if _, err := io.ReadFull(w.out, n[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(n[:])
	if size > maxFrame {
		return nil, errors.New("reply too large")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(w.out, body); err != nil {
		return nil, err
	}
	if status[0] != 0 {
		return nil, fmt.Errorf("worker: %s", body)
	}
	return body, nil
}

type authReport struct {
	Authorized bool   `json:"authorized"`
	Reason     string `json:"reason"`
	Digest     string `json:"digest"`
}

// Authorize decodes the document here, has Rust validate and authorize the same bytes, and requires both to agree.
func (w *Worker) Authorize(doc []byte, env Env) (Authorized, error) {
	un, err := ir.DecodeDocument(doc)
	if err != nil {
		return Authorized{}, refuse("undecodable")
	}
	body := un.Unsafe()
	if body.Family != "operation" || body.Operation == nil {
		return Authorized{}, refuse("not_operation_contract")
	}
	envJSON, _ := json.Marshal(env)
	payload := make([]byte, 4, 4+len(envJSON)+len(doc))
	binary.LittleEndian.PutUint32(payload, uint32(len(envJSON)))
	payload = append(append(payload, envJSON...), doc...)
	raw, err := w.call('X', payload)
	if err != nil {
		return Authorized{}, fmt.Errorf("authorization unavailable: %w", err)
	}
	if _, perr := canon.Parse(raw); perr != nil {
		return Authorized{}, errors.New("authorization unavailable: malformed report")
	}
	var rep authReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return Authorized{}, errors.New("authorization unavailable: malformed report")
	}
	if !rep.Authorized {
		return Authorized{}, refuse(rep.Reason)
	}
	if rep.Digest == "" || rep.Digest != un.Digest() {
		return Authorized{}, refuse("digest_disagreement")
	}
	return Authorized{digest: rep.Digest, contract: *body.Operation, env: env, minted: true}, nil
}
