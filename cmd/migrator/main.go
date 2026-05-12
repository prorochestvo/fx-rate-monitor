package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/prorochestvo/dsninjector"
	"github.com/prorochestvo/loginjector"
	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/seilbekskindirov/monitor/migrations"
	_ "modernc.org/sqlite"
)

var (
	BuildVersion = "dev"
	BuildTime    = "unknown"
	BuildHash    = "undefined"
	LogsDir      = path.Join(os.TempDir(), "logs")
	LogVerbosity = internal.LogLevelWarning
)

const (
	envDsnSqliteDB = "SQLITEDB_DSN"
)

func main() {
	log.Printf("build: %s (%s) at %s\n", BuildVersion, BuildHash, BuildTime)

	l, err := internal.NewLogger(LogsDir, "collector", LogVerbosity)
	if err != nil {
		log.Fatalf("logger: %s", err.Error())
	}
	log.Println("logger: initiated")

	dsnDB, err := dsninjector.Unmarshal(envDsnSqliteDB)
	if err != nil {
		if env := os.Getenv(envDsnSqliteDB); env == "" {
			err = errors.Join(errors.New("environment variable is not set"), err)
		}
		log.Fatalf("settings: %s, %s", envDsnSqliteDB, err.Error())
		return
	}
	log.Println("settings: initiated")

	err = run(dsnDB, l)
	if err != nil {
		log.Printf("migrator: %s", err)
		os.Exit(1)
	}
}

func run(dsnSQLiteDB dsninjector.DataSource, logger *loginjector.Logger) (err error) {

	db, err := sqlitedb.NewSQLiteClient(dsnSQLiteDB, os.Stdout)
	if err != nil {
		return
	}
	defer func() {
		if e := db.Close(); e != nil {
			err = errors.Join(err, fmt.Errorf("close db: %w", e))
		}
	}()

	m, err := sqlitedb.NewMigrator(db, migrations.MigrationsFS)
	if err != nil {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err = m.Run(ctx); err != nil {
		return
	}

	log.Printf("migrator: applied %d migration(s)", m.Applied())

	return
}

func init() {
	logsDir := flag.String("logs-dir", LogsDir, "path to logs directory")
	verbosity := flag.String("verbosity", "warning", "minimum stdout log level (debug, info, warning, error, severe, critical)")
	flag.Parse()

	if dir := *logsDir; dir != "" {
		LogsDir = dir
	}

	if v := *verbosity; v != "" {
		LogVerbosity = internal.ParseLogLevel(*verbosity)
	}
}
