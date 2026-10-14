package testutil

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"tabmail/internal/config"
)

// R5SMTPBehavior describes replies on a real loopback SMTP transport.
// DropFinal closes after actual DATA, before its final reply. DropQUIT closes
// only after DATA was acknowledged; neither is a fake DeliveryAdapter result.
type R5SMTPBehavior struct {
	MailCode, RCPTCode, DataCode, FinalCode int
	DropFinal, DropQUIT                     bool
}
type R5SMTPEvent struct {
	Stage         string
	Code, Bytes   int
	ContentSHA256 string
}
type R5SMTPServer struct {
	listener      net.Listener
	ctx           context.Context
	cancel        context.CancelFunc
	behavior      R5SMTPBehavior
	mu            sync.Mutex
	active        map[net.Conn]bool
	events        []R5SMTPEvent
	wg            sync.WaitGroup
	once          sync.Once
	ready, closed chan struct{}
}

func StartR5SMTP(ctx context.Context, behavior R5SMTPBehavior) (*R5SMTPServer, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("R5 SMTP requires a hard context deadline")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, code := range []int{behavior.MailCode, behavior.RCPTCode, behavior.DataCode, behavior.FinalCode} {
		if code != 0 && (code < 200 || code > 599) {
			return nil, errors.New("invalid R5 SMTP reply code")
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	owned, cancel := context.WithCancel(ctx)
	s := &R5SMTPServer{listener: listener, ctx: owned, cancel: cancel, behavior: behavior, active: map[net.Conn]bool{}, ready: make(chan struct{}), closed: make(chan struct{})}
	s.wg.Add(1)
	go s.accept()
	close(s.ready)
	go func() { <-owned.Done(); _ = s.Close() }()
	return s, nil
}
func (s *R5SMTPServer) Ready() <-chan struct{}  { return s.ready }
func (s *R5SMTPServer) Closed() <-chan struct{} { return s.closed }
func (s *R5SMTPServer) Config() config.Outbound {
	host, port, _ := net.SplitHostPort(s.listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return config.Outbound{Enabled: true, Mode: "relay", RelayHost: host, RelayPort: n, RelayTLS: "none"}
}
func (s *R5SMTPServer) Events() []R5SMTPEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]R5SMTPEvent(nil), s.events...)
}
func (s *R5SMTPServer) record(e R5SMTPEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}
func (s *R5SMTPServer) Close() error {
	s.once.Do(func() {
		s.cancel()
		_ = s.listener.Close()
		s.mu.Lock()
		for conn := range s.active {
			_ = conn.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
		close(s.closed)
	})
	return nil
}
func (s *R5SMTPServer) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.ctx.Err() != nil {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.active[conn] = true
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer conn.Close()
			defer func() { s.mu.Lock(); delete(s.active, conn); s.mu.Unlock() }()
			s.serve(conn)
		}()
	}
}
func (s *R5SMTPServer) serve(conn net.Conn) {
	deadline, _ := s.ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	reader := textproto.NewReader(bufio.NewReader(conn))
	writer := bufio.NewWriter(conn)
	reply := func(stage string, code int) {
		s.record(R5SMTPEvent{Stage: stage, Code: code})
		_, _ = fmt.Fprintf(writer, "%d R5 loopback fixture\r\n", code)
		_ = writer.Flush()
	}
	reply("greeting", 220)
	for {
		line, err := reader.ReadLine()
		if err != nil {
			return
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			reply("empty", 500)
			continue
		}
		command := strings.ToUpper(fields[0])
		switch command {
		case "EHLO", "HELO":
			reply("hello", 250)
		case "MAIL":
			code := s.behavior.MailCode
			if code == 0 {
				code = 250
			}
			reply("mail", code)
		case "RCPT":
			code := s.behavior.RCPTCode
			if code == 0 {
				code = 250
			}
			reply("rcpt", code)
		case "DATA":
			code := s.behavior.DataCode
			if code == 0 {
				code = 354
			}
			reply("data", code)
			if code != 354 {
				continue
			}
			body, err := io.ReadAll(io.LimitReader(reader.DotReader(), 1024*1024+1))
			if err != nil || len(body) > 1024*1024 {
				return
			}
			hash := sha256.Sum256(body)
			s.record(R5SMTPEvent{Stage: "received_data", Bytes: len(body), ContentSHA256: hex.EncodeToString(hash[:])})
			if s.behavior.DropFinal {
				return
			}
			code = s.behavior.FinalCode
			if code == 0 {
				code = 250
			}
			reply("final", code)
		case "QUIT":
			if s.behavior.DropQUIT {
				s.record(R5SMTPEvent{Stage: "quit_dropped"})
				return
			}
			reply("quit", 221)
			return
		case "RSET", "NOOP":
			reply("control", 250)
		default:
			reply("unknown", 500)
		}
	}
}

// Server deadlines and owned cancellation close every accepted socket; no test
// process waits on a worker or leaked stream after Close returns.
