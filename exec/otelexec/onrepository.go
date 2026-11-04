package otelexec

import (
	"context"

	iterator "github.com/jcchavezs/gh-iterator"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OnRepository returns a function to be used as iterator.Options.OnRepository (or
// iterator.ViewRepositoriesOptions.OnRepository) that starts one span per repository.
// The span is ended, recording the error if any, when the iterator is done with the
// repository, so cloning and processing commands traced by Wrap nest under their own
// repository's span.
//
// Only WithTracerProvider is taken into account from the options.
func OnRepository(opts ...Option) func(context.Context, iterator.Repository) (context.Context, func(error)) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.tp == nil {
		cfg.tp = otel.GetTracerProvider()
	}
	tracer := cfg.tp.Tracer(instrumentationName)

	return func(ctx context.Context, repo iterator.Repository) (context.Context, func(error)) {
		ctx, span := tracer.Start(ctx, "process repository",
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(attribute.String("repository.name", repo.Name)))

		return ctx, func(err error) {
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.End()
		}
	}
}
