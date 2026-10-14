package remote

import (
	"context"
	"fmt"
)

// Check verifies what a worker needs before ADE starts its containers. It
// creates the roots rather than demanding them: a Mac has no provisioning step.
func Check(ctx context.Context, host *Host, roots Roots) error {
	if _, err := host.Run(ctx, "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		return fmt.Errorf("docker is not usable on %s: %w — install Docker and allow this ssh user to use its daemon", host.Name, err)
	}
	for _, dir := range []string{roots.Work, roots.Run} {
		if _, err := host.Run(ctx, "mkdir", "-p", dir); err != nil {
			return fmt.Errorf("create %s on %s: %w — grant this ssh user write access to it", dir, host.Name, err)
		}
		if _, err := host.Run(ctx, "test", "-d", dir, "-a", "-w", dir); err != nil {
			return fmt.Errorf("%s is not writable on %s: %w — grant this ssh user write access", dir, host.Name, err)
		}
	}
	return nil
}
