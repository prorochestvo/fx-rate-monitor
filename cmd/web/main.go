// Command web serves the HTTP API and the embedded Mini App static files.
// It reads SQLITEDB_DSN and TELEGRAMBOT_DSN from the environment, starts the
// Telegram bot update loop, and listens on the port configured by --port (default 8080).
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
	"net"
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
	// BuildVersion is the application version string, injected at link time via -ldflags.
	BuildVersion = "dev"
	// BuildTime is the build timestamp, injected at link time via -ldflags.
	BuildTime = "unknown"
	// BuildHash is the VCS commit hash, injected at link time via -ldflags.
	BuildHash = "undefined"
	// LogsDir is the directory where log files are written.
	LogsDir = path.Join(os.TempDir(), "logs")
	// LogVerbosity controls the minimum log level emitted by the logger.
	LogVerbosity = internal.LogLevelWarning
	// HttpPort is the TCP port the HTTP server listens on.
	HttpPort = 8080
	// HttpTimeOut is the read/write/idle timeout for the HTTP server.
	HttpTimeOut = 30 * time.Second
	// StaticDir overrides the embedded static file system when non-empty.
	StaticDir = ""
	// APIDsn is the public HTTPS origin passed via --api-dsn; used by the WASM client.
	APIDsn = ""
)

const (
	envDsnTelegramBOT = "TELEGRAMBOT_DSN"
	envDsnSqliteDB    = "SQLITEDB_DSN"
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
		log.Fatalf("logger: %v", err)
	}
	log.Println("logger: initiated")

	// init settings
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
	dsnDB, err := dsninjector.Unmarshal(envDsnSqliteDB)
	if err != nil {
		log.Fatalf("settings: %s, %s", envDsnSqliteDB, err.Error())
		return
	}
	log.Println("settings: initiated")

	// init dependencies
	db, err := sqlitedb.NewSQLiteClient(dsnDB, l.WriterAs(internal.LogLevelInfo))
	if err != nil {
		log.Fatalf("dependencies: %s", err.Error())
		return
	}
	if err = sqlitedb.RequireMigratedSchema(context.Background(), db); err != nil {
		log.Fatalf("dependencies: schema check: %s", err.Error())
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
	sourceRepo, err := repository.NewRateSourceRepository(db)
	if err != nil {
		log.Fatalf("repositories: %s", err.Error())
	}
	historyRepo, err := repository.NewExecutionHistoryRepository(db)
	if err != nil {
		log.Fatalf("repositories: %s", err.Error())
	}
	rateValueRepo, err := repository.NewRateValueRepository(db)
	if err != nil {
		log.Fatalf("repositories: %s", err.Error())
	}
	subscriptionRepo, err := repository.NewRateUserSubscriptionRepository(db)
	if err != nil {
		log.Fatalf("repositories: %s", err.Error())
	}
	eventRepo, err := repository.NewRateUserEventRepository(db)
	if err != nil {
		log.Fatalf("repositories: %s", err.Error())
	}
	log.Println("repositories: initiated")

	restAPI, err := service.NewRateRestAPI(
		historyRepo,
		sourceRepo,
		rateValueRepo,
		subscriptionRepo,
		eventRepo,
	)
	if err != nil {
		log.Fatalf("services: rest api is failed, %s", err.Error())
		return
	}
	botToken := tbot.BotToken()
	if botToken == "" {
		// Misconfiguration: without a bot token the Mini App initData HMAC
		// cannot be verified, so every /api/me/* call returns 401. Fail at
		// startup instead of silently rejecting every authenticated request.
		log.Fatalf("services: bot token is empty — check TELEGRAMBOT_DSN")
	}
	mux, err := gateway.NewGateway(restAPI, botToken, subscriptionRepo, sourceRepo, rateValueRepo)
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
	tbotAPI, err := service.NewTelegramApi(tbot, subscriptionRepo, sourceRepo, webAppURL)
	if err != nil {
		log.Fatalf("services: telegram api is failed, %s", err.Error())
		return
	}
	log.Println("services: initiated")

	// One signal context drives both the Telegram update loop and the HTTP
	// server's shutdown wait. Sibling binaries (collector, notifier) use the
	// same pattern.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// run telegram server (bound to ctx so SIGTERM cancels the bot poll loop)
	tbotAPI.Run(ctx)

	// run http server. Bind the listener before logging "listening" so the
	// readiness signal fires only after the kernel has bound the port; a
	// monitoring probe that grepped for the marker line previously raced
	// the goroutine and could connect to a not-yet-bound port.
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", HttpPort),
		Handler:      mux,
		ReadTimeout:  HttpTimeOut,
		WriteTimeout: HttpTimeOut,
		IdleTimeout:  HttpTimeOut >> 1,
	}
	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Fatalf("http server: bind %s: %s", srv.Addr, err)
	}
	// srv.Serve closes listener on clean exit; this guards the panic / fatal
	// window between bind and Serve. Double-close on the happy path is a
	// no-op-with-error and the error is intentionally discarded.
	defer func() { _ = listener.Close() }()
	log.Printf("http server: listening on %d port", HttpPort)
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %s", err)
		}
	}()

	log.Println("initialization completed")

	<-ctx.Done()
	log.Println("http server: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server: forced shutdown failed, %s", err)
	}
}

//go:embed static
var staticFS embed.FS

func init() {
	port := flag.Int("port", HttpPort, "http server port")
	timeout := flag.String("timeout", HttpTimeOut.String(), "HTTP read/write/idle timeout duration")
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
