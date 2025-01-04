package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/saint/ghquick/internal/log"
)

type Operations struct {
	workingDir string
	logger     *log.Logger
}

func NewOperations(workingDir string, debug bool) *Operations {
	return &Operations{
		workingDir: workingDir,
		logger:     log.New(debug),
	}
}

func (o *Operations) cleanupLocks() error {
	lockFiles := []string{
		filepath.Join(o.workingDir, ".git", "index.lock"),
		filepath.Join(o.workingDir, ".git", "HEAD.lock"),
	}

	for _, lockFile := range lockFiles {
		if _, err := os.Stat(lockFile); err == nil {
			o.logger.Warning("Found stale lock file: %s", lockFile)
			if err := os.Remove(lockFile); err != nil {
				o.logger.Error("Failed to remove lock file: %s", lockFile)
				return fmt.Errorf("failed to remove lock file %s: %w", lockFile, err)
			}
			o.logger.Success("Removed stale lock file: %s", lockFile)
		}
	}
	return nil
}

func (o *Operations) runCommand(ctx context.Context, name string, args ...string) error {
	// Clean up any stale locks before running git commands
	if name == "git" {
		if err := o.cleanupLocks(); err != nil {
			return err
		}
	}

	o.logger.Command(name, args...)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = o.workingDir
	if output, err := cmd.CombinedOutput(); err != nil {
		o.logger.Debug("Command output: %s", string(output))
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}

func (o *Operations) configureGitUser(ctx context.Context) error {
	o.logger.Step("Configuring git user...")
	cmd := exec.CommandContext(ctx, "git", "config", "--global", "user.name", os.Getenv("GITHUB_USERNAME"))
	cmd.Dir = o.workingDir
	if err := cmd.Run(); err != nil {
		o.logger.Error("Failed to set git username")
		return fmt.Errorf("failed to set git user.name: %w", err)
	}
	o.logger.Success("Git user configured")
	return nil
}

func (o *Operations) EnsureGitSetup(ctx context.Context, repoName string) error {
	// Check if .git directory exists
	gitDir := filepath.Join(o.workingDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		o.logger.Step("Initializing git repository...")
		if err := o.runCommand(ctx, "git", "init"); err != nil {
			o.logger.Error("Failed to initialize git repository")
			return fmt.Errorf("failed to initialize git repository: %w", err)
		}
		o.logger.Success("Git repository initialized")
	} else {
		o.logger.Info("Git repository already initialized")
	}

	// Configure git user
	if err := o.configureGitUser(ctx); err != nil {
		return err
	}

	// Check if remote origin exists
	o.logger.Step("Checking remote configuration...")
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = o.workingDir
	if err := cmd.Run(); err != nil {
		// Add remote origin
		remoteURL := fmt.Sprintf("https://github.com/%s/%s.git", os.Getenv("GITHUB_USERNAME"), repoName)
		o.logger.Step("Adding remote origin: %s", remoteURL)
		if err := o.runCommand(ctx, "git", "remote", "add", "origin", remoteURL); err != nil {
			o.logger.Error("Failed to add remote origin")
			return fmt.Errorf("failed to add remote origin: %w", err)
		}
		o.logger.Success("Remote origin added")
	} else {
		o.logger.Info("Remote origin already configured")
	}

	return nil
}

func (o *Operations) GetDiff(ctx context.Context) (string, error) {
	o.logger.Step("Getting changes...")
	cmd := exec.CommandContext(ctx, "git", "diff", "--cached")
	cmd.Dir = o.workingDir

	output, err := cmd.Output()
	if err != nil {
		// If nothing is staged, get unstaged changes
		o.logger.Debug("No staged changes, checking unstaged changes...")
		cmd = exec.CommandContext(ctx, "git", "diff")
		cmd.Dir = o.workingDir
		output, err = cmd.Output()
		if err != nil {
			o.logger.Error("Failed to get changes")
			return "", fmt.Errorf("failed to get diff: %w", err)
		}
	}

	if len(output) == 0 {
		o.logger.Warning("No changes detected")
	} else {
		o.logger.Success("Changes detected")
	}
	return string(output), nil
}

func (o *Operations) StageAll(ctx context.Context) error {
	o.logger.Step("Staging all changes...")

	// First try git add -A
	if err := o.runCommand(ctx, "git", "add", "-A"); err != nil {
		o.logger.Warning("Failed to stage with -A flag, trying alternative method...")

		// If that fails, try explicit path
		if err := o.runCommand(ctx, "git", "add", o.workingDir); err != nil {
			o.logger.Error("Failed to stage changes")
			return fmt.Errorf("failed to stage files: %w", err)
		}
	}

	// Verify files were staged
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = o.workingDir
	output, err := cmd.Output()
	if err != nil {
		o.logger.Error("Failed to check git status")
		return fmt.Errorf("failed to check git status: %w", err)
	}

	if len(output) == 0 {
		o.logger.Warning("No changes to stage")
		return fmt.Errorf("no changes to commit")
	}

	o.logger.Success("Changes staged")
	o.logger.Debug("Staged files:\n%s", string(output))
	return nil
}

func (o *Operations) Commit(ctx context.Context, message string) error {
	o.logger.Step("Committing changes...")
	if err := o.runCommand(ctx, "git", "commit", "-m", message); err != nil {
		o.logger.Error("Failed to commit changes")
		return fmt.Errorf("failed to commit: %w", err)
	}
	o.logger.Success("Changes committed")
	return nil
}

func (o *Operations) Push(ctx context.Context, remote, branch string) error {
	if remote == "" {
		remote = "origin"
	}
	if branch == "" {
		branch = "main"
	}

	o.logger.Step("Pushing to %s/%s...", remote, branch)
	if err := o.runCommand(ctx, "git", "push", "-u", remote, branch); err != nil {
		o.logger.Error("Failed to push changes")
		return fmt.Errorf("failed to push: %w", err)
	}
	o.logger.Success("Changes pushed successfully")
	return nil
}