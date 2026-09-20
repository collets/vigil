// vigil-guardian is an internal container entrypoint, not a host command.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"vigil/internal/boundary"
)

func main() {
	run := flag.String("run-id", "", "Bound run identity")
	lease := flag.String("lease", "/control/lease.json", "Read-only controller lease")
	uid := flag.Uint("uid", 1000, "Unprivileged worker UID")
	gid := flag.Uint("gid", 1000, "Unprivileged worker GID")
	limit := flag.Duration("limit", 0, "Nonrenewable maximum lifetime")
	flag.Parse()
	if *uid > 1<<32-1 || *gid > 1<<32-1 || *limit < time.Millisecond {
		fmt.Fprintln(os.Stderr, "invalid guardian options")
		os.Exit(2)
	}
	if err := boundary.Guard(*run, *lease, uint32(*uid), uint32(*gid), *limit, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
