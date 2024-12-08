// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otelslog

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"testing"
	"testing/slogtest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
	"go.opentelemetry.io/otel/log/global"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var now = time.Now()

func TestNewLogger(t *testing.T) {
	assert.IsType(t, &Handler{}, NewLogger("").Handler())
}

// embeddedLogger is a type alias so the embedded.Logger type doesn't conflict
// with the Logger method of the recorder when it is embedded.
type embeddedLogger = embedded.Logger // nolint:unused  // Used below.

type scope struct {
	Name, Version, SchemaURL string
}

// recorder records all [log.Record]s it is ased to emit.
type recorder struct {
	embedded.LoggerProvider
	embeddedLogger // nolint:unused  // Used to embed embedded.Logger.

	// Records are the records emitted.
	Records []log.Record

	// Scope is the Logger scope recorder received when Logger was called.
	Scope scope

	// MinSeverity is the minimum severity the recorder will return true for
	// when Enabled is called (unless enableKey is set).
	MinSeverity log.Severity
}

func (r *recorder) Logger(name string, opts ...log.LoggerOption) log.Logger {
	cfg := log.NewLoggerConfig(opts...)

	r.Scope = scope{
		Name:      name,
		Version:   cfg.InstrumentationVersion(),
		SchemaURL: cfg.SchemaURL(),
	}
	return r
}

type enablerKey uint

var enableKey enablerKey

func (r *recorder) Enabled(ctx context.Context, param log.EnabledParameters) bool {
	lvl, ok := param.Severity()
	if !ok {
		return true
	}
	return ctx.Value(enableKey) != nil || lvl >= r.MinSeverity
}

func (r *recorder) Emit(_ context.Context, record log.Record) {
	r.Records = append(r.Records, record)
}

func (r *recorder) Results() []map[string]any {
	out := make([]map[string]any, len(r.Records))
	for i := range out {
		r := r.Records[i]

		m := make(map[string]any)
		if tStamp := r.Timestamp(); !tStamp.IsZero() {
			m[slog.TimeKey] = tStamp
		}
		if lvl := r.Severity(); lvl != 0 {
			m[slog.LevelKey] = lvl - 9
		}
		if body := r.Body(); body.Kind() != log.KindEmpty {
			m[slog.MessageKey] = value2Result(body)
		}
		r.WalkAttributes(func(kv log.KeyValue) bool {
			m[kv.Key] = value2Result(kv.Value)
			return true
		})

		out[i] = m
	}
	return out
}

func value2Result(v log.Value) any {
	switch v.Kind() {
	case log.KindBool:
		return v.AsBool()
	case log.KindFloat64:
		return v.AsFloat64()
	case log.KindInt64:
		return v.AsInt64()
	case log.KindString:
		return v.AsString()
	case log.KindBytes:
		return v.AsBytes()
	case log.KindSlice:
		return v
	case log.KindMap:
		m := make(map[string]any)
		for _, val := range v.AsMap() {
			m[val.Key] = value2Result(val.Value)
		}
		return m
	}
	return nil
}

// testCase represents a complete setup/run/check of an slog handler to test.
// It is based on the testCase from "testing/slogtest" (1.22.1).
type testCase struct {
	// Subtest name.
	name string
	// If non-empty, explanation explains the violated constraint.
	explanation string
	// f executes a single log event using its argument logger.
	// So that mkdescs.sh can generate the right description,
	// the body of f must appear on a single line whose first
	// non-whitespace characters are "l.".
	f func(*slog.Logger)
	// If mod is not nil, it is called to modify the Record
	// generated by the Logger before it is passed to the Handler.
	mod func(*slog.Record)
	// checks is a list of checks to run on the result. Each item is a slice of
	// checks that will be evaluated for the corresponding record emitted.
	checks [][]check
	// options are passed to the Handler constructed for this test case.
	options []Option
}

// copied from slogtest (1.22.1).
type check func(map[string]any) string

// copied from slogtest (1.22.1).
func hasKey(key string) check {
	return func(m map[string]any) string {
		if _, ok := m[key]; !ok {
			return fmt.Sprintf("missing key %q", key)
		}
		return ""
	}
}

// copied from slogtest (1.22.1).
func missingKey(key string) check {
	return func(m map[string]any) string {
		if _, ok := m[key]; ok {
			return fmt.Sprintf("unexpected key %q", key)
		}
		return ""
	}
}

// copied from slogtest (1.22.1).
func hasAttr(key string, wantVal any) check {
	return func(m map[string]any) string {
		if s := hasKey(key)(m); s != "" {
			return s
		}
		gotVal := m[key]
		if !reflect.DeepEqual(gotVal, wantVal) {
			return fmt.Sprintf("%q: got %#v, want %#v", key, gotVal, wantVal)
		}
		return ""
	}
}

// copied from slogtest (1.22.1).
func inGroup(name string, c check) check {
	return func(m map[string]any) string {
		v, ok := m[name]
		if !ok {
			return fmt.Sprintf("missing group %q", name)
		}
		g, ok := v.(map[string]any)
		if !ok {
			return fmt.Sprintf("value for group %q is not map[string]any", name)
		}
		return c(g)
	}
}

// copied from slogtest (1.22.1).
func withSource(s string) string {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		panic("runtime.Caller failed")
	}
	return fmt.Sprintf("%s (%s:%d)", s, file, line)
}

// copied from slogtest (1.22.1).
type wrapper struct {
	slog.Handler
	mod func(*slog.Record)
}

// copied from slogtest (1.22.1).
func (h *wrapper) Handle(ctx context.Context, r slog.Record) error {
	h.mod(&r)
	return h.Handler.Handle(ctx, r)
}

func TestSLogHandler(t *testing.T) {
	// Capture the PC of this line
	pc, file, line, _ := runtime.Caller(0)
	funcName := runtime.FuncForPC(pc).Name()

	cases := []testCase{
		{
			name:        "Values",
			explanation: withSource("all slog Values need to be supported"),
			f: func(l *slog.Logger) {
				l.Info(
					"msg",
					"any", struct{ data int64 }{data: 1},
					"bool", true,
					"duration", time.Minute,
					"float64", 3.14159,
					"int64", -2,
					"string", "str",
					"time", now,
					"uint64", uint64(3),
					"nil", nil,
					"slice", []string{"foo", "bar"},
					// KindGroup and KindLogValuer are left for slogtest.TestHandler.
				)
			},
			checks: [][]check{{
				hasKey(slog.TimeKey),
				hasKey(slog.LevelKey),
				hasAttr("any", "{data:1}"),
				hasAttr("bool", true),
				hasAttr("duration", int64(time.Minute)),
				hasAttr("float64", 3.14159),
				hasAttr("int64", int64(-2)),
				hasAttr("string", "str"),
				hasAttr("time", now.UnixNano()),
				hasAttr("uint64", int64(3)),
				hasAttr("nil", nil),
				hasAttr("slice", log.SliceValue(log.StringValue("foo"), log.StringValue("bar"))),
			}},
		},
		{
			name:        "multi-messages",
			explanation: withSource("this test expects multiple independent messages"),
			f: func(l *slog.Logger) {
				l.Info("one")
				l.Info("two")
			},
			checks: [][]check{{
				hasKey(slog.TimeKey),
				hasKey(slog.LevelKey),
				hasAttr(slog.MessageKey, "one"),
			}, {
				hasKey(slog.TimeKey),
				hasKey(slog.LevelKey),
				hasAttr(slog.MessageKey, "two"),
			}},
		},
		{
			name:        "multi-attrs",
			explanation: withSource("attributes from one message do not affect another"),
			f: func(l *slog.Logger) {
				l.Info("one", "k", "v")
				l.Info("two")
			},
			checks: [][]check{{
				hasAttr("k", "v"),
			}, {
				missingKey("k"),
			}},
		},
		{
			name:        "independent-WithAttrs",
			explanation: withSource("a Handler should only include attributes from its own WithAttr origin"),
			f: func(l *slog.Logger) {
				l1 := l.With("a", "b")
				l2 := l1.With("c", "d")
				l3 := l1.With("e", "f")

				l3.Info("msg", "k", "v")
				l2.Info("msg", "k", "v")
				l1.Info("msg", "k", "v")
				l.Info("msg", "k", "v")
			},
			checks: [][]check{{
				hasAttr("a", "b"),
				hasAttr("e", "f"),
				hasAttr("k", "v"),
			}, {
				hasAttr("a", "b"),
				hasAttr("c", "d"),
				hasAttr("k", "v"),
				missingKey("e"),
			}, {
				hasAttr("a", "b"),
				hasAttr("k", "v"),
				missingKey("c"),
				missingKey("e"),
			}, {
				hasAttr("k", "v"),
				missingKey("a"),
				missingKey("c"),
				missingKey("e"),
			}},
		},
		{
			name:        "independent-WithGroup",
			explanation: withSource("a Handler should only include attributes from its own WithGroup origin"),
			f: func(l *slog.Logger) {
				l1 := l.WithGroup("G").With("a", "b")
				l2 := l1.WithGroup("H").With("c", "d")
				l3 := l1.WithGroup("I").With("e", "f")

				l3.Info("msg", "k", "v")
				l2.Info("msg", "k", "v")
				l1.Info("msg", "k", "v")
				l.Info("msg", "k", "v")
			},
			checks: [][]check{{
				hasKey(slog.TimeKey),
				hasKey(slog.LevelKey),
				hasAttr(slog.MessageKey, "msg"),
				missingKey("a"),
				missingKey("c"),
				missingKey("H"),
				inGroup("G", hasAttr("a", "b")),
				inGroup("G", inGroup("I", hasAttr("e", "f"))),
				inGroup("G", inGroup("I", hasAttr("k", "v"))),
			}, {
				hasKey(slog.TimeKey),
				hasKey(slog.LevelKey),
				hasAttr(slog.MessageKey, "msg"),
				missingKey("a"),
				missingKey("c"),
				inGroup("G", hasAttr("a", "b")),
				inGroup("G", inGroup("H", hasAttr("c", "d"))),
				inGroup("G", inGroup("H", hasAttr("k", "v"))),
			}, {
				hasKey(slog.TimeKey),
				hasKey(slog.LevelKey),
				hasAttr(slog.MessageKey, "msg"),
				missingKey("a"),
				missingKey("c"),
				missingKey("H"),
				inGroup("G", hasAttr("a", "b")),
				inGroup("G", hasAttr("k", "v")),
			}, {
				hasKey(slog.TimeKey),
				hasKey(slog.LevelKey),
				hasAttr("k", "v"),
				hasAttr(slog.MessageKey, "msg"),
				missingKey("a"),
				missingKey("c"),
				missingKey("G"),
				missingKey("H"),
			}},
		},
		{
			name:        "independent-WithGroup.WithAttrs",
			explanation: withSource("a Handler should only include group attributes from its own WithAttr origin"),
			f: func(l *slog.Logger) {
				l = l.WithGroup("G")
				l.With("a", "b").Info("msg", "k", "v")
				l.With("c", "d").Info("msg", "k", "v")
			},
			checks: [][]check{{
				inGroup("G", hasAttr("a", "b")),
				inGroup("G", hasAttr("k", "v")),
				inGroup("G", missingKey("c")),
			}, {
				inGroup("G", hasAttr("c", "d")),
				inGroup("G", hasAttr("k", "v")),
				inGroup("G", missingKey("a")),
			}},
		},
		{
			name:        "WithSource",
			explanation: withSource("a Handler using the WithSource Option should include file attributes from where the log was emitted"),
			f: func(l *slog.Logger) {
				l.Info("msg")
			},
			mod: func(r *slog.Record) {
				// Assign the PC of record to the one captured above.
				r.PC = pc
			},
			checks: [][]check{{
				hasAttr(string(semconv.CodeFilepathKey), file),
				hasAttr(string(semconv.CodeFunctionKey), funcName),
				hasAttr(string(semconv.CodeLineNumberKey), int64(line)),
			}},
			options: []Option{WithSource(true)},
		},
	}

	// Based on slogtest.Run.
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := new(recorder)
			opts := append([]Option{WithLoggerProvider(r)}, c.options...)
			var h slog.Handler = NewHandler("", opts...)
			if c.mod != nil {
				h = &wrapper{h, c.mod}
			}
			l := slog.New(h)
			c.f(l)
			got := r.Results()
			if len(got) != len(c.checks) {
				t.Fatalf("missing record checks: %d records, %d checks", len(got), len(c.checks))
			}
			for i, checks := range c.checks {
				for _, check := range checks {
					if p := check(got[i]); p != "" {
						t.Errorf("%s: %s", p, c.explanation)
					}
				}
			}
		})
	}

	t.Run("slogtest.TestHandler", func(t *testing.T) {
		r := new(recorder)
		h := NewHandler("", WithLoggerProvider(r))

		// TODO: use slogtest.Run when Go 1.21 is no longer supported.
		err := slogtest.TestHandler(h, r.Results)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestNewHandlerConfiguration(t *testing.T) {
	name := "name"
	t.Run("Default", func(t *testing.T) {
		r := new(recorder)
		prev := global.GetLoggerProvider()
		defer global.SetLoggerProvider(prev)
		global.SetLoggerProvider(r)

		var h *Handler
		require.NotPanics(t, func() { h = NewHandler(name) })
		require.NotNil(t, h.logger)
		require.IsType(t, &recorder{}, h.logger)

		l := h.logger.(*recorder)
		want := scope{Name: name}
		assert.Equal(t, want, l.Scope)
	})

	t.Run("Options", func(t *testing.T) {
		r := new(recorder)
		var h *Handler
		require.NotPanics(t, func() {
			h = NewHandler(
				name,
				WithLoggerProvider(r),
				WithVersion("ver"),
				WithSchemaURL("url"),
				WithSource(true),
			)
		})
		require.NotNil(t, h.logger)
		require.IsType(t, &recorder{}, h.logger)

		l := h.logger.(*recorder)
		scope := scope{Name: "name", Version: "ver", SchemaURL: "url"}
		assert.Equal(t, scope, l.Scope)
	})
}

func TestHandlerEnabled(t *testing.T) {
	r := new(recorder)
	r.MinSeverity = log.SeverityInfo

	h := NewHandler("name", WithLoggerProvider(r))

	ctx := context.Background()
	assert.False(t, h.Enabled(ctx, slog.LevelDebug), "level conversion: permissive")
	assert.True(t, h.Enabled(ctx, slog.LevelInfo), "level conversion: restrictive")

	ctx = context.WithValue(ctx, enableKey, true)
	assert.True(t, h.Enabled(ctx, slog.LevelDebug), "context not passed")
}

func BenchmarkHandler(b *testing.B) {
	var (
		h   slog.Handler
		err error
	)

	attrs10 := []slog.Attr{
		slog.String("1", "1"),
		slog.Int64("2", 2),
		slog.Int("3", 3),
		slog.Uint64("4", 4),
		slog.Float64("5", 5.),
		slog.Bool("6", true),
		slog.Time("7", time.Now()),
		slog.Duration("8", time.Second),
		slog.Any("9", 9),
		slog.Any("10", "10"),
	}
	attrs5 := attrs10[:5]
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "body", 0)
	ctx := context.Background()

	b.Run("Handle", func(b *testing.B) {
		handlers := make([]*Handler, b.N)
		for i := range handlers {
			handlers[i] = NewHandler("")
		}

		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			err = handlers[n].Handle(ctx, record)
		}
	})

	b.Run("WithAttrs", func(b *testing.B) {
		b.Run("5", func(b *testing.B) {
			handlers := make([]*Handler, b.N)
			for i := range handlers {
				handlers[i] = NewHandler("")
			}

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				h = handlers[n].WithAttrs(attrs5)
			}
		})
		b.Run("10", func(b *testing.B) {
			handlers := make([]*Handler, b.N)
			for i := range handlers {
				handlers[i] = NewHandler("")
			}

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				h = handlers[n].WithAttrs(attrs10)
			}
		})
	})

	b.Run("WithGroup", func(b *testing.B) {
		handlers := make([]*Handler, b.N)
		for i := range handlers {
			handlers[i] = NewHandler("")
		}

		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			h = handlers[n].WithGroup("group")
		}
	})

	b.Run("WithGroup.WithAttrs", func(b *testing.B) {
		b.Run("5", func(b *testing.B) {
			handlers := make([]*Handler, b.N)
			for i := range handlers {
				handlers[i] = NewHandler("")
			}

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				h = handlers[n].WithGroup("group").WithAttrs(attrs5)
			}
		})
		b.Run("10", func(b *testing.B) {
			handlers := make([]*Handler, b.N)
			for i := range handlers {
				handlers[i] = NewHandler("")
			}

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				h = handlers[n].WithGroup("group").WithAttrs(attrs10)
			}
		})
	})

	b.Run("(WithGroup.WithAttrs).Handle", func(b *testing.B) {
		b.Run("5", func(b *testing.B) {
			handlers := make([]slog.Handler, b.N)
			for i := range handlers {
				handlers[i] = NewHandler("").WithGroup("group").WithAttrs(attrs5)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				err = handlers[n].Handle(ctx, record)
			}
		})
		b.Run("10", func(b *testing.B) {
			handlers := make([]slog.Handler, b.N)
			for i := range handlers {
				handlers[i] = NewHandler("").WithGroup("group").WithAttrs(attrs10)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				err = handlers[n].Handle(ctx, record)
			}
		})
	})

	_, _ = h, err
}