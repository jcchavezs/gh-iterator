package github

import (
	"context"

	iteratorexec "github.com/jcchavezs/gh-iterator/exec"
)

type Step func(ctx context.Context, exec iteratorexec.Execer) error

func CheckoutNewBranchStep(name string) Step {
	return func(ctx context.Context, exec iteratorexec.Execer) error {
		return CheckoutNewBranch(ctx, exec, name)
	}
}

func AddFilesStep(paths ...string) Step {
	return func(ctx context.Context, exec iteratorexec.Execer) error {
		return AddFiles(ctx, exec, paths...)
	}
}

func CommitStep(message string, flags ...string) Step {
	return func(ctx context.Context, exec iteratorexec.Execer) error {
		return Commit(ctx, exec, message, flags...)
	}
}

func PushStep(branchName string, force PushOption) Step {
	return func(ctx context.Context, exec iteratorexec.Execer) error {
		return Push(ctx, exec, branchName, force)
	}
}

func RunSteps(ctx context.Context, exec iteratorexec.Execer, steps ...Step) error {
	for _, step := range steps {
		if err := step(ctx, exec); err != nil {
			return err
		}
	}

	return nil
}
