package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

var ErrServerClosed = errors.New("relay server closed")

type Config struct {
	ListenAddr   string
	UpstreamAddr string
	DialTimeout  time.Duration
}

func (c Config) validate() error {
	if c.ListenAddr == "" {
		return errors.New("listen address is required")
	}
	if c.UpstreamAddr == "" {
		return errors.New("upstream address is required")
	}
	if c.DialTimeout <= 0 {
		return errors.New("dial timeout must be positive")
	}
	return nil
}

type Server struct {
	cfg    Config
	log    *slog.Logger
	ln     net.Listener
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	wg     sync.WaitGroup
	closed bool
}

func NewServer(cfg Config, logger *slog.Logger) (*Server, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid relay config: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{cfg: cfg, log: logger, conns: make(map[net.Conn]struct{})}, nil
}

func (s *Server) Listen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return errors.New("relay server is already listening")
	}
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.ListenAddr, err)
	}
	s.ln = ln
	return nil
}

func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}

func (s *Server) Serve(ctx context.Context) error {
	if err := s.Listen(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		s.closeConnections()
	}()
	defer func() {
		cancel()
		s.closeConnections()
		s.wg.Wait()
	}()

	for {
		client, err := s.ln.Accept()
		if err != nil {
			if s.isClosed() {
				return ErrServerClosed
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				s.log.Warn("temporary accept error", "err", ne)
				continue
			}
			return fmt.Errorf("accept client: %w", err)
		}
		if !s.addConn(client) {
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(ctx, client)
		}()
	}
}

func (s *Server) handle(ctx context.Context, client net.Conn) {
	defer s.removeConn(client)
	defer client.Close()

	dialCtx, cancel := context.WithTimeout(ctx, s.cfg.DialTimeout)
	defer cancel()
	upstream, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", s.cfg.UpstreamAddr)
	if err != nil {
		s.log.Warn("upstream connection failed", "upstream", s.cfg.UpstreamAddr, "err", err)
		return
	}
	defer upstream.Close()
	s.addConn(upstream)
	defer s.removeConn(upstream)

	s.log.Debug("connection established", "upstream", s.cfg.UpstreamAddr)
	if err := proxy(ctx, client, upstream); err != nil {
		s.log.Debug("connection closed", "err", err)
	}
}

func proxy(ctx context.Context, client, upstream net.Conn) error {
	errs := make(chan error, 2)
	go func() {
		_, err := io.Copy(upstream, client)
		errs <- closeWrite(upstream, err)
	}()
	go func() {
		_, err := io.Copy(client, upstream)
		errs <- closeWrite(client, err)
	}()

	var firstErr error
	completed := 0
	select {
	case <-ctx.Done():
		firstErr = ctx.Err()
		_ = client.Close()
		_ = upstream.Close()
		<-errs
		<-errs
		return firstErr
	case err := <-errs:
		firstErr = err
		completed++
	}

	for completed < 2 {
		select {
		case <-ctx.Done():
			_ = client.Close()
			_ = upstream.Close()
			for completed < 2 {
				<-errs
				completed++
			}
		case err := <-errs:
			if firstErr == nil {
				firstErr = err
			}
			completed++
		}
	}
	return firstErr
}

func closeWrite(conn net.Conn, copyErr error) error {
	if tcp, ok := conn.(*net.TCPConn); ok {
		if err := tcp.CloseWrite(); err != nil && copyErr == nil {
			return err
		}
		return copyErr
	}
	// net.Pipe and test doubles do not expose half-close. Closing the
	// connection is the only portable way to unblock the opposite copy.
	_ = conn.Close()
	return copyErr
}

func (s *Server) closeConnections() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	ln := s.ln
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
}

func (s *Server) addConn(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = conn.Close()
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *Server) removeConn(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}
