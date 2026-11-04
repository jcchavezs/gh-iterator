// Package otelexec provides an exec.Execer decorator that creates an
// OpenTelemetry span for every command executed.
package otelexec

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/jcchavezs/gh-iterator/exec/otelexec"

type config struct {
	tp         trace.TracerProvider
	recordArgs bool
	propagate  bool
}

// Option configures the wrapper.
type Option func(*config)

// WithTracerProvider sets the tracer provider. Defaults to the global one.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *config) { c.tp = tp }
}

// WithRecordArgs records the command arguments in the process.command_args
// attribute. Disabled by default as arguments may contain secrets.
func WithRecordArgs(enabled bool) Option {
	return func(c *config) { c.recordArgs = enabled }
}

// WithPropagateToEnv injects the trace context (TRACEPARENT and TRACESTATE)
// into the environment of the executed command so child processes can join
// the trace.
func WithPropagateToEnv(enabled bool) Option {
	return func(c *config) { c.propagate = enabled }
}

type tracedExecer struct {
	next   exec.Execer
	tracer trace.Tracer
	cfg    *config
}

var _ exec.Execer = tracedExecer{}

// Wrap decorates an Execer so every command run creates a span.
func Wrap(next exec.Execer, opts ...Option) exec.Execer {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.tp == nil {
		cfg.tp = otel.GetTracerProvider()
	}

	return tracedExecer{next: next, tracer: cfg.tp.Tracer(instrumentationName), cfg: cfg}
}

func (e tracedExecer) with(next exec.Execer) exec.Execer {
	return tracedExecer{next: next, tracer: e.tracer, cfg: e.cfg}
}

func (e tracedExecer) Run(ctx context.Context, command string, args ...string) (exec.Result, error) {
	return e.RunWithStdin(ctx, nil, command, args...)
}

func (e tracedExecer) RunX(ctx context.Context, command string, args ...string) (string, error) {
	return e.RunWithStdinX(ctx, nil, command, args...)
}

func (e tracedExecer) RunWithStdinX(ctx context.Context, stdin io.Reader, command string, args ...string) (string, error) {
	res, err := e.RunWithStdin(ctx, stdin, command, args...)
	if err != nil {
		return "", err
	}

	return res.Stdout, exec.ResultErr(res, command, args...)
}

func (e tracedExecer) RunWithStdin(ctx context.Context, stdin io.Reader, command string, args ...string) (exec.Result, error) {
	attrs := []attribute.KeyValue{attribute.String("process.command", command)}
	if e.cfg.recordArgs {
		attrs = append(attrs, attribute.StringSlice("process.command_args", args))
	}

	ctx, span := e.tracer.Start(ctx, "exec "+command,
		trace.WithSpanKind(trace.SpanKindInternal), trace.WithAttributes(attrs...))
	defer span.End()

	next := e.next
	if e.cfg.propagate {
		carrier := propagation.MapCarrier{}
		propagation.TraceContext{}.Inject(ctx, carrier)
		var kv []string
		for _, k := range []string{"traceparent", "tracestate"} {
			if v := carrier.Get(k); v != "" {
				kv = append(kv, strings.ToUpper(k), v)
			}
		}
		next = next.WithEnv(kv...)
	}

	res, err := next.RunWithStdin(ctx, stdin, command, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return res, err
	}

	span.SetAttributes(
		attribute.Int("process.exit_code", res.ExitCode),
		attribute.Bool("process.cancelled", res.Cancelled),
	)
	if res.ExitCode != 0 {
		span.SetStatus(codes.Error, "non-zero exit code")
	}

	return res, nil
}

func (e tracedExecer) Log(ctx context.Context, level slog.Level, msg string, fields ...any) {
	e.next.Log(ctx, level, msg, fields...)
}

func (e tracedExecer) DebugShell(ctx context.Context) { e.next.DebugShell(ctx) }

func (e tracedExecer) WithEnv(kv ...string) exec.Execer { return e.with(e.next.WithEnv(kv...)) }

func (e tracedExecer) WithLogFields(kvFields ...any) exec.Execer {
	return e.with(e.next.WithLogFields(kvFields...))
}

func (e tracedExecer) Sub(subpath string) (exec.Execer, error) {
	sub, err := e.next.Sub(subpath)
	if err != nil {
		return nil, err
	}

	return e.with(sub), nil
}

func (e tracedExecer) GenerateFS() afero.Fs { return e.next.GenerateFS() }
