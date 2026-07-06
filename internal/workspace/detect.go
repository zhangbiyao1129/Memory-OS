package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CommandRunner interface {
	Output(ctx context.Context, dir string, name string, args ...string) (string, error)
}

type ExecCommandRunner struct{}

func (ExecCommandRunner) Output(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

type GitDetector struct {
	runner CommandRunner
}

func NewGitDetector(runner CommandRunner) GitDetector {
	if runner == nil {
		runner = ExecCommandRunner{}
	}
	return GitDetector{runner: runner}
}

func DetectGit(ctx context.Context, cwd string) (Identity, error) {
	return NewGitDetector(ExecCommandRunner{}).Detect(ctx, cwd)
}

func (d GitDetector) Detect(ctx context.Context, cwd string) (Identity, error) {
	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return Identity{}, err
		}
		cwd = current
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return Identity{}, err
	}
	root, err := d.required(ctx, absCWD, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return localIdentity(absCWD), nil
	}
	remote, err := d.required(ctx, absCWD, "git", "config", "--get", "remote.origin.url")
	if err != nil {
		return localIdentity(absCWD), nil
	}
	sourceKey, err := NormalizeGitRemote(remote)
	if err != nil {
		return Identity{}, err
	}
	branch := d.optional(ctx, absCWD, "git", "branch", "--show-current")
	commit := d.optional(ctx, absCWD, "git", "rev-parse", "HEAD")
	return Identity{
		CWD:       absCWD,
		GitRoot:   root,
		GitRemote: sourceKey,
		GitBranch: branch,
		GitCommit: commit,
	}, nil
}

func localIdentity(absCWD string) Identity {
	localPath := strings.TrimLeft(filepath.ToSlash(absCWD), "/")
	if localPath == "" {
		localPath = "workspace"
	}
	return Identity{
		CWD:       absCWD,
		GitRoot:   absCWD,
		GitRemote: "local/" + localPath,
	}
}

func (d GitDetector) required(ctx context.Context, dir string, name string, args ...string) (string, error) {
	output, err := d.runner.Output(ctx, dir, name, args...)
	if err != nil {
		return "", err
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return "", errors.New("empty command output")
	}
	return output, nil
}

func (d GitDetector) optional(ctx context.Context, dir string, name string, args ...string) string {
	output, err := d.runner.Output(ctx, dir, name, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}
