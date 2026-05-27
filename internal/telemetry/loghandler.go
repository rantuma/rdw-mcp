package telemetry

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

type (
	traceContextHandler struct {
		slog.Handler
	}
	fanoutHandler struct {
		handlers []slog.Handler
	}
)

func NewLogHandler(inner slog.Handler) slog.Handler {
	return traceContextHandler{Handler: inner}
}

func (handler traceContextHandler) Handle(ctx context.Context, rec slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		rec.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}

	return handler.Handler.Handle(ctx, rec)
}

func (handler traceContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceContextHandler{Handler: handler.Handler.WithAttrs(attrs)}
}

func (handler traceContextHandler) WithGroup(name string) slog.Handler {
	return traceContextHandler{Handler: handler.Handler.WithGroup(name)}
}

func NewFanoutHandler(handlers ...slog.Handler) slog.Handler {
	if len(handlers) == 1 {
		return handlers[0]
	}

	return fanoutHandler{handlers: handlers}
}

func (handler fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range handler.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}

	return false
}

func (handler fanoutHandler) Handle(ctx context.Context, rec slog.Record) error {
	var errs []error
	for _, h := range handler.handlers {
		if !h.Enabled(ctx, rec.Level) {
			continue
		}
		if err := h.Handle(ctx, rec.Clone()); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (handler fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	derived := make([]slog.Handler, len(handler.handlers))
	for i, h := range handler.handlers {
		derived[i] = h.WithAttrs(attrs)
	}

	return fanoutHandler{handlers: derived}
}

func (handler fanoutHandler) WithGroup(name string) slog.Handler {
	derived := make([]slog.Handler, len(handler.handlers))
	for i, h := range handler.handlers {
		derived[i] = h.WithGroup(name)
	}

	return fanoutHandler{handlers: derived}
}
