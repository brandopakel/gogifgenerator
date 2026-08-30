package cinematic

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type CommandStageOptions struct {
	Descriptor StageDescriptor
	Executable string
	Directory  string
	Timeout    time.Duration
	Arguments  func(Job) []string
	Validate   func(Job) error
}

type CommandStage struct {
	descriptor StageDescriptor
	executable string
	directory  string
	timeout    time.Duration
	arguments  func(Job) []string
	validate   func(Job) error
}

func NewCommandStage(options CommandStageOptions) (*CommandStage, error) {
	if strings.TrimSpace(options.Descriptor.ID) == "" || strings.TrimSpace(options.Descriptor.Label) == "" || strings.TrimSpace(options.Descriptor.Role) == "" {
		return nil, errors.New("cinematic: command stage descriptor is incomplete")
	}
	if options.Arguments == nil || options.Validate == nil {
		return nil, errors.New("cinematic: command stage requires arguments and output validation")
	}
	resolved, err := exec.LookPath(strings.TrimSpace(options.Executable))
	if err != nil {
		return nil, fmt.Errorf("%w: find %s: %v", ErrUnavailable, options.Descriptor.Label, err)
	}
	if options.Timeout <= 0 {
		options.Timeout = 10 * time.Minute
	}
	options.Descriptor.Available = true
	options.Descriptor.Local = true
	return &CommandStage{
		descriptor: options.Descriptor, executable: resolved, directory: options.Directory,
		timeout: options.Timeout, arguments: options.Arguments, validate: options.Validate,
	}, nil
}

func (s *CommandStage) Descriptor() StageDescriptor { return s.descriptor }

func (s *CommandStage) Run(ctx context.Context, job Job) error {
	if s == nil || s.executable == "" {
		return errors.New("command stage is not configured")
	}
	stageContext, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	command := exec.CommandContext(stageContext, s.executable, s.arguments(job)...)
	command.Dir = s.directory
	var diagnostics commandOutput
	command.Stdout = &diagnostics
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		if stageContext.Err() != nil {
			return fmt.Errorf("timed out: %w", stageContext.Err())
		}
		return fmt.Errorf("command failed: %w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	return s.validate(job)
}

type commandOutput struct{ data []byte }

func (b *commandOutput) Write(data []byte) (int, error) {
	const limit = 64 << 10
	if remaining := limit - len(b.data); remaining > 0 {
		b.data = append(b.data, data[:min(remaining, len(data))]...)
	}
	return len(data), nil
}

func (b *commandOutput) String() string { return string(b.data) }
