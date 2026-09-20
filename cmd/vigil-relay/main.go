// vigil-relay is an internal provider sidecar. Its socket volume is shared
// read-only with an isolated worker; only this process receives provider auth.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"vigil/internal/boundary"
)

func run() error {
	socket := flag.String("socket", "/relay/provider.sock", "Selected provider socket")
	limit := flag.Duration("limit", 0, "Nonrenewable relay lifetime")
	flag.Parse()
	if runtime.GOOS != "linux" || os.Getuid() == 0 || flag.NArg() != 0 || *limit <= 0 || *limit > 90*time.Minute {
		return fmt.Errorf("relay requires non-root Linux execution and an explicit bounded lifetime")
	}
	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, *limit)
	defer cancel()
	type result struct {
		config boundary.RelayBootstrap
		err    error
	}
	boot := make(chan result, 1)
	go func() { config, err := boundary.ReadRelayBootstrap(os.Stdin); boot <- result{config, err} }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	var config boundary.RelayBootstrap
	select {
	case r := <-boot:
		if r.err != nil {
			return r.err
		}
		config = r.config
	case <-ctx.Done():
		return fmt.Errorf("relay startup cancelled")
	case <-timer.C:
		return fmt.Errorf("relay bootstrap deadline exceeded")
	}
	relay, err := boundary.NewRelay(ctx, config.Upstream, config.Model, config.RunToken, config.ProviderKey)
	if err != nil {
		return fmt.Errorf("invalid relay configuration")
	}
	defer relay.Close()
	listener, err := boundary.ListenRelaySocket(*socket)
	if err != nil {
		return fmt.Errorf("cannot create relay socket")
	}
	defer listener.Close()
	fmt.Println(`{"ready":true}`)
	if err := boundary.ServeProvider(ctx, listener, relay); err != nil {
		return fmt.Errorf("relay transport failed")
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
