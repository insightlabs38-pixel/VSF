package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os/exec"
	"time"
)

// ErrUnavailable means the OS keychain cannot be reached; a vault that fails this way stores nothing.
var ErrUnavailable = errors.New("auth.keychain_unavailable")

// Runner runs a command with the secret on its standard input, never on its command line.
type Runner interface {
	Run(ctx context.Context, stdin []byte, name string, args ...string) (stdout []byte, exit int, err error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), ee.ExitCode(), nil
	}
	return out.Bytes(), 0, err
}

// SecretService is the freedesktop Secret Service (GNOME Keyring, KWallet) reached through secret-tool.
type SecretService struct {
	Run     Runner
	Service string
	Timeout time.Duration
}

func (k *SecretService) runner() Runner {
	if k.Run != nil {
		return k.Run
	}
	return execRunner{}
}

func (k *SecretService) ctx() (context.Context, context.CancelFunc) {
	t := k.Timeout
	if t <= 0 {
		t = 5 * time.Second
	}
	return context.WithTimeout(context.Background(), t)
}

func (k *SecretService) attrs(account, name string) []string {
	svc := k.Service
	if svc == "" {
		svc = "vsf"
	}
	return []string{"service", svc, "account", account, "name", name}
}

func (k *SecretService) Put(account, name string, value []byte) error {
	if !idRe.MatchString(account) || !nameRe.MatchString(name) || len(value) > maxSecret {
		return ErrInvalid
	}
	ctx, cancel := k.ctx()
	defer cancel()
	enc := []byte(base64.StdEncoding.EncodeToString(value))
	_, exit, err := k.runner().Run(ctx, enc, "secret-tool", append([]string{"store", "--label=VSF"}, k.attrs(account, name)...)...)
	if err != nil || exit != 0 {
		return ErrUnavailable
	}
	return nil
}

func (k *SecretService) Get(account, name string) ([]byte, error) {
	if !idRe.MatchString(account) || !nameRe.MatchString(name) {
		return nil, ErrInvalid
	}
	ctx, cancel := k.ctx()
	defer cancel()
	out, exit, err := k.runner().Run(ctx, nil, "secret-tool", append([]string{"lookup"}, k.attrs(account, name)...)...)
	if err != nil {
		return nil, ErrUnavailable
	}
	if exit != 0 || len(bytes.TrimSpace(out)) == 0 {
		return nil, ErrNotFound
	}
	plain, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(out)))
	if err != nil {
		return nil, ErrUntrusted
	}
	return plain, nil
}

func (k *SecretService) Delete(account, name string) error {
	if !idRe.MatchString(account) || !nameRe.MatchString(name) {
		return ErrInvalid
	}
	ctx, cancel := k.ctx()
	defer cancel()
	_, exit, err := k.runner().Run(ctx, nil, "secret-tool", append([]string{"clear"}, k.attrs(account, name)...)...)
	if err != nil {
		return ErrUnavailable
	}
	_ = exit
	return nil
}

// Chain stores in the primary vault, and in the fallback only when the primary is unavailable and the fallback was allowed on purpose. Without that, an unreachable keychain means nothing is stored.
type Chain struct {
	Primary, Fallback Vault
	AllowFallback     bool
}

func (c *Chain) Put(account, name string, v []byte) error {
	err := c.Primary.Put(account, name, v)
	if !errors.Is(err, ErrUnavailable) {
		return err
	}
	if !c.AllowFallback || c.Fallback == nil {
		return ErrUnavailable
	}
	return c.Fallback.Put(account, name, v)
}

func (c *Chain) Get(account, name string) ([]byte, error) {
	b, err := c.Primary.Get(account, name)
	if err == nil || (!errors.Is(err, ErrUnavailable) && !errors.Is(err, ErrNotFound)) {
		return b, err
	}
	if !c.AllowFallback || c.Fallback == nil {
		return nil, err
	}
	if fb, ferr := c.Fallback.Get(account, name); ferr == nil {
		return fb, nil
	}
	return nil, err
}

func (c *Chain) Delete(account, name string) error {
	err := c.Primary.Delete(account, name)
	if c.Fallback != nil {
		if ferr := c.Fallback.Delete(account, name); ferr != nil && err == nil {
			err = ferr
		}
	}
	return err
}
