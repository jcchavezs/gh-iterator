package otelexec_test

import (
	"context"
	"testing"

	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/otelexec"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setup(t *testing.T, opts ...otelexec.Option) (exec.Execer, *tracetest.SpanRecorder) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	opts = append(opts, otelexec.WithTracerProvider(tp))
	return otelexec.Wrap(exec.NewExecer(t.TempDir()), opts...), sr
}

func attrMap(kvs []attribute.KeyValue) map[attribute.Key]attribute.Value {
	m := map[attribute.Key]attribute.Value{}
	for _, kv := range kvs {
		m[kv.Key] = kv.Value
	}
	return m
}

func TestRunX_SingleSpan(t *testing.T) {
	e, sr := setup(t)
	out, err := e.RunX(context.Background(), "echo", "hello")
	require.NoError(t, err)
	require.Equal(t, "hello\n", out)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "exec echo", spans[0].Name())
	attrs := attrMap(spans[0].Attributes())
	require.Equal(t, "echo", attrs["process.command"].AsString())
	require.EqualValues(t, 0, attrs["process.exit_code"].AsInt64())
	require.NotContains(t, attrs, attribute.Key("process.command_args"))
}

func TestRecordArgs(t *testing.T) {
	e, sr := setup(t, otelexec.WithRecordArgs(true))
	_, err := e.Run(context.Background(), "echo", "a", "b")
	require.NoError(t, err)
	attrs := attrMap(sr.Ended()[0].Attributes())
	require.Equal(t, []string{"a", "b"}, attrs["process.command_args"].AsStringSlice())
}

func TestNonZeroExit(t *testing.T) {
	e, sr := setup(t)
	_, err := e.RunX(context.Background(), "sh", "-c", "exit 3")
	require.Error(t, err)
	var xe interface{ ExitCode() int }
	require.ErrorAs(t, err, &xe)
	require.Equal(t, 3, xe.ExitCode())

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.EqualValues(t, 3, attrMap(spans[0].Attributes())["process.exit_code"].AsInt64())
}

func TestRunError(t *testing.T) {
	e, sr := setup(t)
	_, err := e.Run(context.Background(), "definitely-not-a-command")
	require.Error(t, err)
	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.NotEmpty(t, spans[0].Events())
}

func TestChildrenStayTraced(t *testing.T) {
	e, sr := setup(t)
	e = e.WithEnv("A", "b").WithLogFields("k", "v")
	_, err := e.Run(context.Background(), "true")
	require.NoError(t, err)
	require.Len(t, sr.Ended(), 1)
}

func TestSubStaysTraced(t *testing.T) {
	e, sr := setup(t)
	_, err := e.Sub("missing")
	require.Error(t, err)

	_, err = e.Run(context.Background(), "mkdir", "d")
	require.NoError(t, err)
	sub, err := e.Sub("d")
	require.NoError(t, err)
	_, err = sub.Run(context.Background(), "true")
	require.NoError(t, err)
	require.Len(t, sr.Ended(), 2)
}

func TestPropagateToEnv(t *testing.T) {
	e, sr := setup(t, otelexec.WithPropagateToEnv(true))
	out, err := e.RunX(context.Background(), "sh", "-c", "echo $TRACEPARENT")
	require.NoError(t, err)

	sc := sr.Ended()[0].SpanContext()
	require.Contains(t, out, sc.TraceID().String())
	require.Contains(t, out, sc.SpanID().String())
}

func TestNestsUnderParent(t *testing.T) {
	e, sr := setup(t)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	ctx, parent := tp.Tracer("t").Start(context.Background(), "parent")
	_, err := e.Run(ctx, "true")
	require.NoError(t, err)
	parent.End()

	spans := sr.Ended()
	require.Len(t, spans, 2)
	require.Equal(t, parent.SpanContext().SpanID(), spans[0].Parent().SpanID())
}
