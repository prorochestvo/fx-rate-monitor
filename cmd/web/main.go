package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/prorochestvo/dsninjector"
	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/application/service"
	"github.com/seilbekskindirov/monitor/internal/gateway"
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
	LogVerbosity = internal.LogLevelWarning
	HttpPort     = 8080
	HttpTimeOut  = 30 * time.Second
	StaticDir    = ""
	APIDsn       = ""
)

const (
	envDsnSqliteDB    = "SQLITEDB_DSN"
	envDsnTelegramBOT = "TELEGRAMBOT_DSN"
)

func main() {
	log.Printf("build: %s (%s) at %s\n", BuildVersion, BuildHash, BuildTime)
	if StaticDir != "" {
		log.Printf("static directory (override): %s\n", StaticDir)
	} else {
		log.Println("static directory: embedded FS")
	}

	l, err := internal.NewLogger(LogsDir, "web", LogVerbosity)
	if err != nil {
		log.Fatalf("logger init: %v", err)
	}

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
	if APIDsn == "" {
		log.Fatalf("settings: --api-dsn is required (format: https://<host>/)")
	}
	dsnAPI, err := dsninjector.Parse(APIDsn)
	if err != nil {
		log.Fatalf("settings: --api-dsn, %s", err.Error())
		return
	}
	// Telegram WebApp buttons reject non-HTTPS, IP literals, and localhost,
	// so the DSN's host must resolve to a publicly reachable HTTPS host.
	webAppURL := "https://" + strings.TrimPrefix(strings.TrimPrefix(dsnAPI.Addr(), "https://"), "http://") + "/app/subscriptions.html"
	log.Println("settings: initiated")

	// init dependencies
	db, err := sqlitedb.NewSQLiteClient(dsnSQLiteDB, l.WriterAs(internal.LogLevelInfo))
	if err != nil {
		log.Fatalf("dependencies: sqlite %s connection is failed, %s", dsnSQLiteDB.Database(), err.Error())
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
	if id, username, err := tbot.Me(context.Background()); err != nil {
		log.Printf("telegram: identity probe failed: %v", err)
	} else {
		log.Printf("telegram: authenticated as @%s (id=%d)", username, id)
	}
	log.Println("dependencies: initiated")

	// init repositories
	rRateSource, err := repository.NewRateSourceRepository(db)
	if err != nil {
		log.Fatalf("rate source repo: %s", err)
	}
	rExecutionHistory, err := repository.NewExecutionHistoryRepository(db)
	if err != nil {
		log.Fatalf("execution history repo: %s", err)
	}
	rRateValue, err := repository.NewRateValueRepository(db)
	if err != nil {
		log.Fatalf("rate value repo: %s", err)
	}
	rRateUserSubscription, err := repository.NewRateUserSubscriptionRepository(db)
	if err != nil {
		log.Fatalf("repositories: user subscription build is failed, %s", err.Error())
		return
	}
	rRateUserEvent, err := repository.NewRateUserEventRepository(db)
	if err != nil {
		log.Fatalf("repositories: notification pool build is failed, %s", err.Error())
		return
	}
	log.Println("repositories: initiated")

	restAPI, err := service.NewRateRestAPI(
		rExecutionHistory,
		rRateSource,
		rRateValue,
		rRateUserSubscription,
		rRateUserEvent,
	)
	if err != nil {
		log.Fatalf("services: rest api is failed, %s", err.Error())
		return
	}
	botToken := tbot.BotToken()
	mux, err := gateway.NewGateway(restAPI, botToken, rRateUserSubscription, rRateSource, rRateValue)
	if err != nil {
		log.Fatalf("services: mux api is failed, %s", err.Error())
		return
	}
	var fsys http.FileSystem
	if StaticDir != "" {
		fsys = http.Dir(StaticDir)
	} else {
		sub, err := fs.Sub(staticFS, "static")
		if err != nil {
			log.Fatalf("embed sub: %v", err)
		}
		fsys = http.FS(sub)
	}
	mux.Handle("/", http.FileServer(fsys))
	tbotAPI, err := service.NewTelegramApi(tbot, rRateUserSubscription, rRateSource, webAppURL)
	if err != nil {
		log.Fatalf("services: telegram api is failed, %s", err.Error())
		return
	}
	log.Println("services: initiated")

	// run telegram server
	tbotAPI.Run(context.Background())

	// run http server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", HttpPort),
		Handler:      mux,
		ReadTimeout:  HttpTimeOut,
		WriteTimeout: HttpTimeOut,
		IdleTimeout:  HttpTimeOut >> 1,
	}
	go func() {
		log.Printf("http server: listening on %d port", HttpPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %s", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	time.Sleep(10 * time.Millisecond)

	log.Println("initialization completed")

	<-quit
	log.Println("http server: shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("http server: forced shutdown failed, %s", err)
	}
}

//go:embed static
var staticFS embed.FS

func init() {
	port := flag.Int("port", HttpPort, "http server port")
	timeout := flag.String("timeout", HttpTimeOut.String(), "HTTP server timeout duration")
	logsDir := flag.String("logs-dir", LogsDir, "path to logs directory")
	verbosity := flag.String("verbosity", "warning", "minimum stdout log level (debug, info, warning, error, severe, critical)")
	staticDir := flag.String("static-dir", StaticDir, "path to static files directory")
	apiDsn := flag.String("api-dsn", APIDsn, "public HTTPS origin DSN, format: https://<host>/")
	flag.Parse()

	if *port <= 1000 || *port >= 32000 {
		log.Printf("invalid port value: %d, using default %d", *port, HttpPort)
	} else {
		HttpPort = *port
	}

	if value, err := time.ParseDuration(*timeout); err != nil {
		log.Printf("invalid timeout value: %s, using default %s", *timeout, HttpTimeOut.String())
	} else if value > 10*time.Second {
		HttpTimeOut = value
	}

	if dir := *staticDir; dir != "" {
		StaticDir = dir
	}

	if dir := *logsDir; dir != "" {
		LogsDir = dir
	}

	if v := *verbosity; v != "" {
		LogVerbosity = internal.ParseLogLevel(*verbosity)
	}

	if v := *apiDsn; v != "" {
		APIDsn = v
	}
}
