package otelexec_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	iterator "github.com/jcchavezs/gh-iterator"
	"github.com/jcchavezs/gh-iterator/exec"
	"github.com/jcchavezs/gh-iterator/exec/otelexec"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// fakeGH puts a fake gh on the PATH answering the repository lookup with an empty
// repository, which is processed without cloning, keeping the tests offline.
func fakeGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nfor a; do last=$a; done\nprintf '{\"full_name\":\"%s\",\"size\":0}' \"${last#/repos/}\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestOnRepository_SpanPerRepository(t *testing.T) {
	fakeGH(t)
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	ctx, parent := tp.Tracer("t").Start(context.Background(), "run")
	endParent := parent.End

	boom := errors.New("boom")
	opts := iterator.Options{
		OnRepository: otelexec.OnRepository(otelexec.WithTracerProvider(tp)),
		WrapExecer: func(x exec.Execer) exec.Execer {
			return otelexec.Wrap(x, otelexec.WithTracerProvider(tp))
		},
	}

	for _, name := range []string{"o/a", "o/b"} {
		err := iterator.RunForRepository(ctx, name, func(ctx context.Context, r iterator.Repository, x exec.Execer) error {
			if _, err := x.RunX(ctx, "true"); err != nil {
				return err
			}
			if r.Name == "o/b" {
				return boom
			}
			return nil
		}, opts)
		if name == "o/b" {
			require.ErrorIs(t, err, boom)
		} else {
			require.NoError(t, err)
		}
	}
	endParent()

	byName := map[string][]sdktrace.ReadOnlySpan{}
	for _, s := range sr.Ended() {
		byName[s.Name()] = append(byName[s.Name()], s)
	}
	repoSpans := byName["process repository"]
	require.Len(t, repoSpans, 2)
	require.Len(t, byName["exec true"], 2)
	require.Len(t, byName["exec gh"], 2)
	run := byName["run"][0]

	status := map[string]codes.Code{}
	for _, rs := range repoSpans {
		// siblings under the run span, not nested in one another
		require.Equal(t, run.SpanContext().SpanID(), rs.Parent().SpanID())
		status[rs.Attributes()[0].Value.AsString()] = rs.Status().Code
	}
	require.Equal(t, codes.Unset, status["o/a"])
	require.Equal(t, codes.Error, status["o/b"])

	// the lookup happens before a repository exists so it sits under the run span
	for _, gs := range byName["exec gh"] {
		require.Equal(t, run.SpanContext().SpanID(), gs.Parent().SpanID())
	}

	// each exec span belongs to its own repository span
	parents := map[string]bool{}
	for _, es := range byName["exec true"] {
		parents[es.Parent().SpanID().String()] = true
	}
	require.Len(t, parents, 2)
	for _, rs := range repoSpans {
		require.True(t, parents[rs.SpanContext().SpanID().String()])
	}
}
