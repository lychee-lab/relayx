package codex

import (
	"context"
	"io"
	"os/exec"
)

type ProcessOptions struct {
	Bin string
}

type processReadWriteCloser struct {
	stdout io.ReadCloser
	stdin  io.WriteCloser
	close  func() error
}

func (p *processReadWriteCloser) Read(b []byte) (int, error) {
	return p.stdout.Read(b)
}

func (p *processReadWriteCloser) Write(b []byte) (int, error) {
	return p.stdin.Write(b)
}

func (p *processReadWriteCloser) Close() error {
	_ = p.stdin.Close()
	_ = p.stdout.Close()
	if p.close != nil {
		return p.close()
	}
	return nil
}

func StartAppServer(ctx context.Context, opts ProcessOptions) (*JSONRPCAdapter, error) {
	bin := opts.Bin
	if bin == "" {
		bin = "codex"
	}

	cmd := exec.CommandContext(ctx, bin, "app-server", "--listen", "stdio://")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return NewJSONRPCAdapter(&processReadWriteCloser{
		stdout: stdout,
		stdin:  stdin,
		close:  cmd.Wait,
	}), nil
}
