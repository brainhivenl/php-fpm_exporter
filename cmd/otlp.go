package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

type PHPError struct {
	ErrorClass string `json:"error_class"`
	Message    string `json:"message"`
	FilePath   string `json:"file_path"`
	Line       int    `json:"line"`
	Trace      string `json:"trace"`
	RequestURL string `json:"request_url"`
	Timestamp  int64  `json:"timestamp"`
}

var tracerProvider *sdktrace.TracerProvider

func init() {
	ctx := context.Background()
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		log.Error("failed to create OTLP exporter:", err)
		return
	}

	tracerProvider = sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
	otel.SetTracerProvider(tracerProvider)
}

func errorsHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()

	if err != nil {
		http.Error(w, "failed to read request", http.StatusBadRequest)
		return
	}

	var phpError PHPError
	if err := json.Unmarshal(body, &phpError); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	tracer := otel.Tracer("php-errors")

	ctx, span := tracer.Start(ctx, "php.exception")
	defer span.End()

	span.AddEvent("exception",
		trace.WithAttributes(
			semconv.ExceptionType(phpError.ErrorClass),
			semconv.ExceptionMessage(phpError.Message),
			semconv.ExceptionStacktrace(phpError.Trace),
		),
	)

	span.SetAttributes(
		attribute.String("code.filepath", phpError.FilePath),
		attribute.Int("code.lineno", phpError.Line),
	)

	if phpError.RequestURL != "" {
		span.SetAttributes(
			attribute.String("http.url", phpError.RequestURL),
		)
	}

	span.SetStatus(codes.Error, fmt.Sprintf("%s: %s", phpError.ErrorClass, phpError.Message))

	log.Infof("php error traced: %s at %s:%d", phpError.ErrorClass, phpError.FilePath, phpError.Line)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("error traced"))
}
