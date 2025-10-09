package sysgo

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/ethereum-optimism/optimism/op-devstack/devtest"
	"github.com/ethereum-optimism/optimism/op-service/logpipe"
)

// SubProcess is a process that can be started, and stopped, and restarted.
//
// If at any point the process fails to start or exit successfully,
// the failure is reported to the devtest.P.
//
// If the sub-process exits by itself, the exit is detected,
// and if not successful (non-zero exit code on unix) it also reports failure to the devtest.P.
//
// Sub-process logs are assumed to be structured JSON logs, and are piped to the logger.
type SubProcess struct {
	p   devtest.P
	cmd *exec.Cmd

	stdOutLogs logpipe.LogProcessor
	stdErrLogs logpipe.LogProcessor

	wg sync.WaitGroup

	mu sync.Mutex
}

func NewSubProcess(p devtest.P, stdOutLogs, stdErrLogs logpipe.LogProcessor) *SubProcess {
	return &SubProcess{
		p:          p,
		stdOutLogs: stdOutLogs,
		stdErrLogs: stdErrLogs,
	}
}

func (sp *SubProcess) Start(cmdPath string, args []string, env []string) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.cmd != nil {
		return fmt.Errorf("process is still running (PID: %d)", sp.cmd.Process.Pid)
	}
	cmd := exec.Command(cmdPath, args...)
	cmd.Env = append(os.Environ(), env...)
	stdout, err := cmd.StdoutPipe()
	sp.p.Require().NoError(err, "stdout err")
	stderr, err := cmd.StderrPipe()
	sp.p.Require().NoError(err, "stderr err")

	sp.wg.Add(1)
	go func() {
		defer sp.wg.Done()
		err := logpipe.PipeLogs(stdout, sp.stdOutLogs)
		sp.p.Require().NoError(err, "stdout logging error")
	}()
	sp.wg.Add(1)
	go func() {
		defer sp.wg.Done()
		err := logpipe.PipeLogs(stderr, sp.stdErrLogs)
		sp.p.Require().NoError(err, "stderr logging error")
	}()
	if err := cmd.Start(); err != nil {
		return err
	}

	sp.cmd = cmd
	sp.p.Cleanup(func() {
		err := sp.Stop()
		if err != nil {
			sp.p.Logger().Error("Shutdown error", "err", err)
		}
	})

	return nil
}

// Stop sends an interrupt and waits for the process to stop.
func (sp *SubProcess) Stop() error {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.cmd == nil {
		return nil // already stopped gracefully
	}

	// If not already done, then try an interrupt first.
	if sp.cmd.ProcessState == nil {
		sp.p.Logger().Info("Sending interrupt")
		if err := sp.cmd.Process.Signal(os.Interrupt); err != nil {
			return err
		}
	}

	sp.wg.Wait() // Wait for stdout and stderr to close so all output is captured.

	_, err := sp.cmd.Process.Wait()
	if err != nil {
		sp.p.Logger().Warn("Sub-process exited with error", "err", err)
	} else {
		sp.p.Logger().Info("Sub-process gracefully exited")
	}

	sp.cmd = nil
	return nil
}

// Wait waits for the process to finish. Stop still must be called to release resources.
func (sp *SubProcess) Wait() {
	sp.wg.Wait()
}
