/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package os

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
)

// CommandRun executes some text as a command. This is assumed to be
// text that will be run by a shell so we need to write out the
// command to a temporary file and then ask the shell to execute
// it, after which the temporary file is removed.
func CommandRun(commandText string, env []string, arguments ...string) error {
	log.Infof("CommandRun(%v,%+v)", commandText, arguments)
	output, err := commandRunContext(context.Background(), commandText, env, 0, arguments...)
	log.Infof("CommandRun: %s\n", output)
	if err != nil {
		return log.Errore(fmt.Errorf("(%s) %s", err.Error(), output))
	}
	return nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	if buffer.limit <= 0 {
		_, _ = buffer.buffer.Write(data)
		return original, nil
	}
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = buffer.buffer.Write(data)
	}
	return original, nil
}

func (buffer *limitedBuffer) String() string {
	return buffer.buffer.String()
}

// CommandRunContext runs a shell hook with cancellation and bounded captured
// output. The returned output is suitable for recovery audit records.
func CommandRunContext(ctx context.Context, commandText string, env []string, outputLimit int, arguments ...string) (string, error) {
	return commandRunContext(ctx, commandText, env, outputLimit, arguments...)
}

func commandRunContext(ctx context.Context, commandText string, env []string, outputLimit int, arguments ...string) (string, error) {
	cmd, shellScript, err := generateShellScriptContext(ctx, commandText, env, arguments...)
	if shellScript != "" {
		defer os.Remove(shellScript)
	}
	if err != nil {
		return "", log.Errore(err)
	}

	output := &limitedBuffer{limit: outputLimit}
	cmd.Stdout, cmd.Stderr = output, output
	err = cmd.Run()
	cmdOutput := output.String()
	if err != nil {
		// Did the command fail because of an unsuccessful exit code
		if exitError, ok := errors.AsType[*exec.ExitError](err); ok {
			if waitStatus, ok := exitError.Sys().(syscall.WaitStatus); ok {
				log.Errorf("hook command failed with exit status %d", waitStatus.ExitStatus())
			}
		}

		return cmdOutput, fmt.Errorf("%s", err.Error())
	}

	return cmdOutput, nil
}

func generateShellScriptContext(ctx context.Context, commandText string, env []string, arguments ...string) (*exec.Cmd, string, error) {
	shell := config.Config.Hooks.ShellCommand

	commandBytes := []byte(commandText)
	tmpFile, err := os.CreateTemp("", "orchestrator-process-cmd-")
	if err != nil {
		return nil, "", log.Errorf("generateShellScript() failed to create TempFile: %v", err.Error())
	}
	// write commandText to temporary file
	os.WriteFile(tmpFile.Name(), commandBytes, 0640)
	shellArguments := append([]string{}, tmpFile.Name())
	shellArguments = append(shellArguments, arguments...)

	cmd := exec.CommandContext(ctx, shell, shellArguments...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.Env = env

	return cmd, tmpFile.Name(), nil
}
