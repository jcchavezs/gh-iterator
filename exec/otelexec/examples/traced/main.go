package main

// This example prints the resulting spans to stdout.
//
// Without arguments it wraps a plain Execer with otelexec and shows nesting under a
// parent span, a failing command, a Sub execer that stays traced, and trace context
// propagation to the child process env.
//
// With a repository name (e.g. jcchavezs/gh-iterator, requires gh) it plugs otelexec into
// the iterator: one span per repository containing the clone and processor commands.

import (
	"context"
	"fmt"
	"os"

	iterator "github.com/jcchavezs/gh-iterator"
	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/otelexec"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	ctx := context.Background()

	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
	defer func() { _ = tp.Shutdown(ctx) }()

	if len(os.Args) > 1 {
		runIterator(ctx, tp, os.Args[1])
		return
	}

	dir, err := os.MkdirTemp("", "otelexec-example")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir) //nolint:errcheck

	e := otelexec.Wrap(
		exec.NewExecer(dir), // any exec.Execer can be wrapped
		otelexec.WithTracerProvider(tp),
		otelexec.WithRecordArgs(true),     // off by default, args may contain secrets
		otelexec.WithPropagateToEnv(true), // children receive TRACEPARENT
	)

	// Spans created by the wrapper nest under the span found in ctx.
	ctx, parent := tp.Tracer("example").Start(ctx, "my-operation")
	defer parent.End()

	// Successful command.
	out, err := e.RunX(ctx, "echo", "hello")
	fmt.Printf("echo: %q err=%v\n", out, err)

	// The child process sees the trace context of its span.
	out, _ = exec.TrimStdout(e.RunX(ctx, "sh", "-c", "echo $TRACEPARENT"))
	fmt.Println("TRACEPARENT seen by child:", out)

	// Non-zero exit: the span status is set to Error.
	_, err = e.RunX(ctx, "sh", "-c", "echo boom >&2; exit 2")
	fmt.Println("failing:", err)

	// Derived execers stay traced.
	_, _ = e.Run(ctx, "mkdir", "sub")
	if sub, err := e.WithEnv("FOO", "bar").Sub("sub"); err == nil {
		_, _ = sub.Run(ctx, "pwd")
	}
}

func runIterator(ctx context.Context, tp *sdktrace.TracerProvider, repo string) {
	// The run span is the parent of every repository span.
	ctx, run := tp.Tracer("example").Start(ctx, "my-run")
	defer run.End()

	err := iterator.RunForRepository(ctx, repo,
		func(ctx context.Context, r iterator.Repository, x exec.Execer) error {
			// x is already traced thanks to WrapExecer.
			out, err := exec.TrimStdout(x.RunX(ctx, "git", "log", "-1", "--format=%h %s"))
			fmt.Printf("%s: %s\n", r.Name, out)
			return err
		},
		iterator.Options{
			// One "process repository" span per repository, ended when the iterator is done with it.
			OnRepository: otelexec.OnRepository(otelexec.WithTracerProvider(tp)),
			// Traces the iterator's own commands (gh, git clone) and the processor's.
			WrapExecer: func(x exec.Execer) exec.Execer {
				return otelexec.Wrap(x, otelexec.WithTracerProvider(tp))
			},
		})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
