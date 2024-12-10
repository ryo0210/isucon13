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
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

// 参考というかコピペ
// https://developer.hatenastaff.com/entry/2024/10/16/180559

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
	bsp := trace.NewBatchSpanProcessor(traceExporter)

	tracerProvider := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSpanProcessor(bsp),
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
	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter)),
		metric.WithResource(res),
	)
	otel.SetMeterProvider(meterProvider)
	return meterProvider.Shutdown, nil
}

func initLoggerProvider(ctx context.Context, res *resource.Resource) (func(context.Context) error, error) {
	logExporter, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create log exporter: %w", err)
	}
	processor := log.NewBatchProcessor(logExporter)
	loggerProvider := log.NewLoggerProvider(
		log.WithProcessor(processor),
		log.WithResource(res),
	)
	global.SetLoggerProvider(loggerProvider)
	return loggerProvider.Shutdown, nil
}
