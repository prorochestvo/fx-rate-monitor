package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"path"

	"github.com/prorochestvo/dsninjector"
	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/application/collection"
	"github.com/seilbekskindirov/monitor/internal/application/notification"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	integration "github.com/seilbekskindirov/monitor/internal/infrastructure/telegrambot"
	"github.com/seilbekskindirov/monitor/internal/repository"
	_ "modernc.org/sqlite"
)

var (
	BuildVersion = "dev"
	BuildTime    = "unknown"
	BuildHash    = "undefined"
	LogsDir      = path.Join(os.TempDir(), "logs")
	ProxyURL     = os.Getenv(envProxyUrl)
	LogVerbosity = internal.LogLevelWarning
)

const (
	envProxyUrl       = "PROXY_URL"
	envDsnTelegramBOT = "TELEGRAMBOT_DSN"
	envDsnSqliteDB    = "SQLITEDB_DSN"
)

func main() {
	log.Printf("build: %s (%s) at %s\n", BuildVersion, BuildHash, BuildTime)

	// init logger
	l, err := internal.NewLogger(LogsDir, "collector", LogVerbosity)
	if err != nil {
		log.Fatalf("logger: %s", err.Error())
	}
	log.Println("logger: initiated")

	// init settings
	dsnSQLiteDB, err := dsninjector.Unmarshal(envDsnSqliteDB)
	if err != nil {
		log.Fatalf("settings: %s, %s", envDsnSqliteDB, err.Error())
		return
	}
	dsnTelegramBOT, err := dsninjector.Unmarshal(envDsnTelegramBOT)
	if err != nil {
		log.Fatalf("settings: %s, %s", envDsnTelegramBOT, err.Error())
		return
	}
	log.Println("settings: initiated")

	// init dependencies
	db, err := sqlitedb.NewSQLiteClient(dsnSQLiteDB, l.WriterAs(internal.LogLevelInfo))
	if err != nil {
		log.Fatalf("dependencies: sqlite connection is failed, %s", err.Error())
		return
	}
	defer func(c io.Closer) {
		if e := c.Close(); e != nil {
			log.Printf("close sqlite client: %v", e)
		}
	}(db)
	tbot, err := integration.NewTBotClient(dsnTelegramBOT, l.WriterAs(internal.LogLevelInfo))
	if err != nil {
		log.Fatalf("dependencies: telegram bot connection is failed, %s", err.Error())
		return
	}
	log.Println("dependencies: initiated")

	// init repositories
	rRateSource, err := repository.NewRateSourceRepository(db)
	if err != nil {
		log.Fatalf("repositories: rate source build is failed, %s", err.Error())
		return
	}
	rExecutionHistory, err := repository.NewExecutionHistoryRepository(db)
	if err != nil {
		log.Fatalf("repositories: execution history build is failed, %s", err.Error())
		return
	}
	rRateValue, err := repository.NewRateValueRepository(db)
	if err != nil {
		log.Fatalf("repositories: rate value build is failed, %s", err.Error())
		return
	}
	rRateUserSubscription, err := repository.NewRateUserSubscriptionRepository(db)
	if err != nil {
		log.Fatalf("repositories: subscription build is failed, %s", err.Error())
		return
	}
	rRateUserEvent, err := repository.NewRateUserEventRepository(db)
	if err != nil {
		log.Fatalf("repositories: notification pool build is failed, %s", err.Error())
		return
	}
	log.Println("repositories: initiated")

	runners, err := buildRunners(
		context.Background(),
		tbot,
		rRateSource,
		rExecutionHistory,
		rRateValue,
		rRateUserSubscription,
		rRateUserEvent,
		l.WriterAs(internal.LogLevelWarning),
	)
	if err != nil {
		log.Fatalf("runners: runners building is failed: %s", err)
		return
	}
	log.Println("runners: initiated")

	ctx := context.Background()

	//// Start the Telegram bot subscription handler (long-polling) in a background goroutine.
	//subHandler := apptelegram.NewSubscriptionHandler(tbot, rRateUserSubscription, rRateSource)
	//go func() {
	//	tbot.Listen(ctx, subHandler.Handle)
	//	log.Println("telegram: bot listener stopped")
	//}()
	////ctx, cancel := context.WithTimeout(ctx, 7*time.Minute)
	////defer cancel()
	//
	////// Dispatch pending/failed notifications and vacuum old records at the start of each cycle.
	////if dispatchErr := a.notificationPool.Dispatch(ctx); dispatchErr != nil {
	////	log.Printf("notification pool: dispatch error: %s\n", dispatchErr)
	////}
	////if vacuumErr := a.notificationPool.Vacuum(ctx); vacuumErr != nil {
	////	log.Printf("notification pool: vacuum error: %s\n", vacuumErr)
	////}

	errs := make([]error, 0, len(runners))
	for _, r := range runners {
		if err = r.Run(ctx); err != nil {
			errs = append(errs, r.Run(ctx))
		}
	}
	if err = errors.Join(errs...); err != nil {
		log.Fatalf("execution:  %s", err)
	}

	log.Println("execution: done")
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

// runner is the minimal interface that rateJob needs from its extractor.
type runner interface {
	Run(context.Context) error
}

func buildRunners(
	ctx context.Context,
	tbot *integration.TelegramBotClient,
	rRateSource *repository.RateSourceRepository,
	rExecutionHistory *repository.ExecutionHistoryRepository,
	rRateValue *repository.RateValueRepository,
	rRateUserSubscription *repository.RateUserSubscriptionRepository,
	rRateUserEvent *repository.RateUserEventRepository,
	logger io.Writer,
) ([]runner, error) {
	collectionRateAgent, err := collection.NewRateAgent(
		ProxyURL,
		rRateSource,
		rExecutionHistory,
		rRateValue,
		rRateUserSubscription,
		rRateUserEvent,
		logger,
	)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	notificationRateAgent, err := notification.NewRateAgent(
		tbot,
		rRateUserEvent,
	)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	err = notificationRateAgent.Vacuum(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	return []runner{collectionRateAgent, notificationRateAgent}, nil
}
