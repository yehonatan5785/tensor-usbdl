package tensorutils

import (
	"bytes"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"
)

type writeResult struct {
	n   int
	err error
}

type fakeSerialPort struct {
	writes      []writeResult
	drains      []error
	written     []byte
	writeInputs [][]byte
	drainCalls  int
}

func (p *fakeSerialPort) Read([]byte) (int, error)           { return 0, nil }
func (p *fakeSerialPort) SetReadTimeout(time.Duration) error { return nil }
func (p *fakeSerialPort) Write(b []byte) (int, error) {
	p.writeInputs = append(p.writeInputs, append([]byte(nil), b...))
	if len(p.writes) == 0 {
		p.written = append(p.written, b...)
		return len(b), nil
	}
	r := p.writes[0]
	p.writes = p.writes[1:]
	if r.n > 0 && r.n <= len(b) {
		p.written = append(p.written, b[:r.n]...)
	}
	return r.n, r.err
}
func (p *fakeSerialPort) Drain() error {
	p.drainCalls++
	if len(p.drains) == 0 {
		return nil
	}
	err := p.drains[0]
	p.drains = p.drains[1:]
	return err
}
func (p *fakeSerialPort) Close() error { return nil }

func TestDNWWriteRetriesInterruptedAndShortWrites(t *testing.T) {
	payload := []byte("abcdefgh")
	p := &fakeSerialPort{writes: []writeResult{
		{err: syscall.EINTR},
		{n: 3, err: syscall.EINTR},
		{err: syscall.EINTR},
		{n: 2},
	}, drains: []error{syscall.EINTR, nil}}
	dnw := &DNW{port: p}

	n, err := dnw.write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("write = %d, %v; want %d, nil", n, err, len(payload))
	}
	if !bytes.Equal(p.written, payload) {
		t.Fatalf("written %q; want %q", p.written, payload)
	}
	if !bytes.Equal(p.writeInputs[2], payload[3:]) || !bytes.Equal(p.writeInputs[3], payload[3:]) || !bytes.Equal(p.writeInputs[4], payload[5:]) {
		t.Fatalf("retry slices did not preserve offset: %#v", p.writeInputs)
	}
	if p.drainCalls != 2 {
		t.Fatalf("Drain called %d times; want 2", p.drainCalls)
	}
}

func TestDNWWriteShortWriteZeroProgress(t *testing.T) {
	p := &fakeSerialPort{writes: []writeResult{{n: 2}, {n: 0}}}
	n, err := (&DNW{port: p}).write([]byte("abcd"))
	if n != 2 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("write = %d, %v; want 2, io.ErrShortWrite", n, err)
	}
	if p.drainCalls != 0 {
		t.Fatalf("Drain called after incomplete write")
	}
}

func TestDNWWriteNonInterruptedErrorPreservesProgress(t *testing.T) {
	wantErr := errors.New("port failed")
	p := &fakeSerialPort{writes: []writeResult{{n: 2, err: wantErr}}}
	n, err := (&DNW{port: p}).write([]byte("abcd"))
	if n != 2 || !errors.Is(err, wantErr) {
		t.Fatalf("write = %d, %v; want 2, port error", n, err)
	}
	if p.drainCalls != 0 {
		t.Fatalf("Drain called after failed write")
	}
}

func TestDNWWriteNonInterruptedDrainErrorPreservesFullWrite(t *testing.T) {
	wantErr := errors.New("drain failed")
	p := &fakeSerialPort{drains: []error{wantErr}}
	n, err := (&DNW{port: p}).write([]byte("abcd"))
	if n != 4 || !errors.Is(err, wantErr) {
		t.Fatalf("write = %d, %v; want 4, drain error", n, err)
	}
}
