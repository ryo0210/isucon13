package main

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type shutdownFunc func(context.Context) error

func InitOtelProvider(ctx context.Context) (shutdownFunc, error) {
	res, err := resource.New(
		ctx,
		resource.WithFromEnv(),
		// resource.WithHost(),
		// resource.WithHostID(),
		resource.WithProcess(),
		resource.WithContainer(),
		resource.WithContainerID(),
		// resource.WithOS(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, err
	}

	shutdownTracerProvider, err := initTracerProvider(ctx, res)
	if err != nil {
		return nil, err
	}

	shutdownMeterProvider, err := initMeterProvider(ctx, res)
	if err != nil {
		return nil, err
	}

	shutdownLoggerProvider, err := initLoggerProvider(ctx, res)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) error {
		if err := shutdownTracerProvider(ctx); err != nil {
			return fmt.Errorf("failed to shutdown TracerProvider: %w", err)
		}
		if err := shutdownMeterProvider(ctx); err != nil {
			return fmt.Errorf("failed to shutdown MeterProvider: %w", err)
		}
		if err := shutdownLoggerProvider(ctx); err != nil {
			return fmt.Errorf("failed to shutdown LoggerProvider: %w", err)
		}
		return nil
	}, nil
}

func initTracerProvider(ctx context.Context, res *resource.Resource) (func(context.Context) error, error) {
	traceExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint("localhost:4318"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tracerProvider.Shutdown, nil
}

func initMeterProvider(ctx context.Context, res *resource.Resource) (func(context.Context) error, error) {
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics exporter: %w", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(meterProvider)
	return meterProvider.Shutdown, nil
}

func initLoggerProvider(ctx context.Context, res *resource.Resource) (func(context.Context) error, error) {
	logExporter, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create log exporter: %w", err)
	}
	processor := sdklog.NewBatchProcessor(logExporter)
	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(processor),
		sdklog.WithResource(res),
	)
	global.SetLoggerProvider(loggerProvider)
	return loggerProvider.Shutdown, nil
}
