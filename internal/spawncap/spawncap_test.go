package spawncap

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobsIsHalfTheCoresAndNeverUnderTwo(t *testing.T) {
	assert.Equal(t, 5, jobs(11))
	assert.Equal(t, 2, jobs(2))
	assert.Equal(t, 2, jobs(1))
}

func TestApplyCapsGoWithoutLosingWhatWasThere(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want []string
	}{
		{"no flags", []string{"HOME=/h"}, []string{"HOME=/h", "GOFLAGS=-p=4"}},
		{"other flags kept", []string{"GOFLAGS=-mod=mod", "HOME=/h"}, []string{"GOFLAGS=-mod=mod -p=4", "HOME=/h"}},
		{"your own -p wins", []string{"GOFLAGS=-p=1"}, []string{"GOFLAGS=-p=1"}},
		{"a bare -p wins too", []string{"GOFLAGS=-p 8"}, []string{"GOFLAGS=-p 8"}},
		{"not fooled by -pkgdir", []string{"GOFLAGS=-pkgdir=/x"}, []string{"GOFLAGS=-pkgdir=/x -p=4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, apply(tt.env, 4))
		})
	}
}

func TestApplyLeavesTheCallersSliceAlone(t *testing.T) {
	env := make([]string, 1, 4)
	env[0] = "GOFLAGS=-v"
	_ = apply(env, 4)
	assert.Equal(t, []string{"GOFLAGS=-v"}, env)
}

func TestCountReadsOnlyTheGroupAsked(t *testing.T) {
	out := []byte("  100\n  100\n 1001\n  100\n    1\n")
	assert.Equal(t, 3, count(out, 100))
	assert.Equal(t, 0, count(out, 10))
}

func TestCountSeesARealGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & sleep 30 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()

	assert.Eventually(t, func() bool {
		n, err := Count(context.Background(), cmd.Process.Pid)
		return err == nil && n == 3
	}, 3e9, 5e7)

	n, err := Count(context.Background(), os.Getpid()+1<<22)
	require.NoError(t, err)
	assert.Zero(t, n)
}
