// Package inspector implements the health-check inspector pattern for the web binary.
// Each external dependency registers itself as an Inspector; the Agent aggregates their
// results under a single bounded context timeout so one slow or dead dependency cannot
// hang the endpoint or hide the status of the others.
package inspector

import (
	"context"
	"time"
)

// inspectorTimeout is the whole-sweep budget for a single Agent.CheckUp call.
// Any dependency that does not respond within this window is reported as failing.
const inspectorTimeout = 3 * time.Second

// Inspector is the contract every health-checked dependency must satisfy.
// Name returns a stable, unique label shown in the /health/check report.
// CheckUP performs a real, cheap, read-only probe and returns nil on success
// or the failure reason as an error.
type Inspector interface {
	Name() string
	CheckUP(ctx context.Context) error
}

// Agent runs all registered inspectors under a single bounded context timeout.
type Agent struct {
	inspectors []Inspector
	timeout    time.Duration
}

// NewAgent constructs an Agent that probes each inspector within timeout.
// When timeout is zero or negative, inspectorTimeout (3 s) is used.
func NewAgent(timeout time.Duration, inspectors ...Inspector) *Agent {
	if timeout <= 0 {
		timeout = inspectorTimeout
	}
	return &Agent{inspectors: inspectors, timeout: timeout}
}

// CheckUp probes every registered inspector under a single deadline and returns a
// per-component report. One slow or failing check never prevents the others from
// running. Returns healthy=true iff every inspector returned nil; the report maps
// each inspector's Name() to "ok" or the verbatim error message.
func (a *Agent) CheckUp(ctx context.Context) (healthy bool, report map[string]string) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	healthy = true
	report = make(map[string]string, len(a.inspectors))
	for _, insp := range a.inspectors {
		name := insp.Name()
		if name == "" {
			continue
		}
		if err := insp.CheckUP(ctx); err != nil {
			report[name] = err.Error()
			healthy = false
			continue
		}
		report[name] = "ok"
	}
	return healthy, report
}

// dbPinger is the subset of *sqlitedb.SQLiteClient used by DBInspector.
// Defined here so tests can substitute a fake without importing the concrete type.
type dbPinger interface {
	Ping(ctx context.Context) error
}

// DBInspector wraps a SQLite client and adapts it to the Inspector interface.
// It delegates to the client's Ping method, which performs a PingContext followed
// by a SELECT 1 inside a rolled-back transaction.
type DBInspector struct {
	client dbPinger
}

// NewDBInspector returns an Inspector backed by the given SQLite client.
func NewDBInspector(client dbPinger) *DBInspector {
	return &DBInspector{client: client}
}

// Name returns the label used in the /health/check report.
func (d *DBInspector) Name() string { return "sqlite" }

// CheckUP delegates to the underlying Ping.
func (d *DBInspector) CheckUP(ctx context.Context) error {
	return d.client.Ping(ctx)
}

// botPinger is the subset of *telegrambot.TelegramBotClient used by TelegramInspector.
// Defined here so tests can substitute a fake without importing the concrete type.
type botPinger interface {
	Ping(ctx context.Context) error
}

// TelegramInspector wraps a Telegram bot client and adapts it to the Inspector interface.
// It delegates to the client's Ping method, which calls GetMe and asserts a non-zero
// bot ID. Note: the underlying tgbotapi.BotAPI call does not honour the context, so
// the probe is bounded at the Agent sweep level rather than the individual HTTP call.
type TelegramInspector struct {
	client botPinger
}

// NewTelegramInspector returns an Inspector backed by the given Telegram bot client.
func NewTelegramInspector(client botPinger) *TelegramInspector {
	return &TelegramInspector{client: client}
}

// Name returns the label used in the /health/check report.
func (t *TelegramInspector) Name() string { return "telegram" }

// CheckUP delegates to the underlying Ping.
func (t *TelegramInspector) CheckUP(ctx context.Context) error {
	return t.client.Ping(ctx)
}
