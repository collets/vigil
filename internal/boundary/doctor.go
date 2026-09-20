// Package boundary reports prerequisites; user-declared capabilities cannot turn
// an unqualified native profile into an enforced production boundary.
package boundary

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Report struct {
	Platform           string   `json:"platform"`
	DockerReachable    bool     `json:"docker_reachable"`
	DockerVersion      string   `json:"docker_version,omitempty"`
	ProductionEligible bool     `json:"production_eligible"`
	Reasons            []string `json:"reasons"`
}

func Inspect(ctx context.Context) Report {
	r := Report{Platform: runtime.GOOS + "/" + runtime.GOARCH, Reasons: []string{"complete production harness/profile qualification has not been recorded", "persisted production dispatch is not implemented"}}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Os}} {{.Server.Version}}").Output()
	if err == nil {
		fields := strings.Fields(string(b))
		if len(fields) == 2 && fields[0] == "linux" {
			r.DockerReachable = true
			r.DockerVersion = fields[1]
		}
	}
	if !r.DockerReachable {
		r.Reasons = append(r.Reasons, "no reachable Linux Docker engine; enable WSL integration or configure a runtime")
	}
	return r
}
