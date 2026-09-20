// vigil-worker is the unprivileged container-side provider bridge and native
// process wrapper. It provides no host authorization or containment by itself.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"vigil/internal/boundary"
)

func main() {
	socket := flag.String("socket", "/relay/provider.sock", "Selected provider Unix socket")
	flag.Parse()
	if runtime.GOOS != "linux" || os.Getuid() == 0 || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "worker requires non-root Linux execution and a native command")
		os.Exit(2)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot start provider bridge")
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer listener.Close()
	go func() {
		if boundary.ServeProvider(ctx, listener, boundary.ProviderBridge(*socket)) != nil {
			cancel()
		}
	}()
	cmd := exec.CommandContext(ctx, flag.Arg(0), flag.Args()[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "OPENAI_BASE_URL=") {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	cmd.Env = append(cmd.Env, "OPENAI_BASE_URL=http://"+listener.Addr().String()+"/v1")
	if err = cmd.Run(); err != nil {
		cancel()
		fmt.Fprintln(os.Stderr, "native worker command failed")
		os.Exit(1)
	}
}
