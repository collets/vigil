package boundary

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"time"
)

// ListenRelaySocket creates a single-run socket, refusing existing paths. Workers
// receive only a read-only mount of this socket, never its writable parent.
func ListenRelaySocket(path string) (net.Listener, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("absolute relay socket path required")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return nil, errors.New("relay socket path must not exist")
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// ProviderBridge runs inside the worker network namespace. Its transport always
// dials the selected Unix socket, regardless of request URL or proxy variables.
func ProviderBridge(socket string) http.Handler {
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socket)
	}, DisableKeepAlives: true}
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme = "http"
			r.Out.URL.Host = "provider"
			r.Out.Host = "provider"
			r.Out.Header.Del("Forwarded")
			r.Out.Header.Del("X-Forwarded-For")
			r.Out.Header.Del("X-Forwarded-Host")
			r.Out.Header.Del("X-Forwarded-Proto")
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			relayError(w, 502, "provider bridge unavailable")
		},
		FlushInterval: -1,
	}
}

func ServeProvider(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	stop := context.AfterFunc(ctx, func() { server.Close() })
	defer stop()
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}
