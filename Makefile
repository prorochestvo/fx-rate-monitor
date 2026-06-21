-include .env

SHELL := /bin/bash

ROOT_DIRECTORY := $(shell pwd)
PROJECT_NAME := $(shell basename "$(PWD)")
VERSION := $(shell git describe --tags --always)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
BUILD := $(shell git rev-parse --short HEAD)
TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

BUILD_OPTIONS := "-s -w -X main.BuildVersion=${BRANCH} -X main.BuildTime=${TIME} -X main.BuildHash=${BUILD}"

.PHONY: all run build build-collector build-notifier build-web build-wasm build-migrator migrate test lint format audit audit-help doctor-help swagger clean deploy_environments backups



## deploy_environments: push nginx, systemd unit and cron-script configs to the host, then reload systemd and nginx
deploy_environments:
	scp -r ./configs/nginx.kz_behappy.conf be-happy.kz:/etc/nginx/sites-available/kz.be-happy
	scp -r ./configs/nginx.kz_behappy_balancer_prime.conf be-happy.kz:/etc/nginx/configurations/kz.be-happy.balancer_prime.conf
	scp -r ./configs/nginx.kz_behappy_balancer_stage.conf be-happy.kz:/etc/nginx/configurations/kz.be-happy.balancer_stage.conf
	scp -r ./configs/nginx.kz_behappy_services_prime.conf be-happy.kz:/etc/nginx/configurations/kz.be-happy.services_prime.conf
	scp -r ./configs/nginx.kz_behappy_services_stage.conf be-happy.kz:/etc/nginx/configurations/kz.be-happy.services_stage.conf
	scp -r ./configs/nginx.kz_behappy_common_settings.conf be-happy.kz:/etc/nginx/snippets/kz.be-happy.common_settings.conf
	scp -r ./configs/nginx.kz_behappy_gzip.conf be-happy.kz:/etc/nginx/snippets/kz.be-happy.gzip.conf
	scp -r ./configs/nginx.kz_behappy_certificates.conf be-happy.kz:/etc/nginx/certificates/kz.be-happy.conf
	scp -r ./configs/certbot.com_lingocrm.sh be-happy.kz:/opt/letsencrypt/kz.be-happy
	scp -r ./configs/sqlite_dump.sh be-happy.kz:/opt/monitor/backups/sqlite_dump.sh
	scp -r ./configs/sqlite_dump.env.example be-happy.kz:/tmp/sqlite_dump.env.example
	ssh be-happy.kz "if [ -f /opt/monitor/backups/.env ]; then echo 'skip: /opt/monitor/backups/.env already exists'; rm -f /tmp/sqlite_dump.env.example; else mv /tmp/sqlite_dump.env.example /opt/monitor/backups/.env && chmod 600 /opt/monitor/backups/.env && echo 'installed /opt/monitor/backups/.env from example (edit RCLONE_CONFIG)'; fi"
	scp -r ./configs/srv.prime_monitor.service be-happy.kz:/tmp/service.prime_monitor
	scp -r ./configs/srv.stage_monitor.service be-happy.kz:/tmp/service.stage_monitor
	ssh -t be-happy.kz "sudo sh -c '\
		mv /tmp/service.prime_monitor /etc/systemd/system/prime_monitor.service && \
		mv /tmp/service.stage_monitor /etc/systemd/system/stage_monitor.service && \
		systemctl daemon-reload && \
		nginx -t && nginx -s reload'"
	echo "done"



## run: apply migrations, then start migrator, collector, notifier and web locally from source
run: migrate
	@set -a; . .env; set +a; CGO_ENABLED=0 go run -ldflags ${BUILD_OPTIONS} ./cmd/migrator/main.go   --logs-dir ./build/logs
	@set -a; . .env; set +a; CGO_ENABLED=0 go run -ldflags ${BUILD_OPTIONS} ./cmd/collector/main.go  --logs-dir ./build/logs
	@set -a; . .env; set +a; CGO_ENABLED=0 go run -ldflags ${BUILD_OPTIONS} ./cmd/notifier/main.go   --logs-dir ./build/logs
	@set -a; . .env; set +a; CGO_ENABLED=0 go run -ldflags ${BUILD_OPTIONS} ./cmd/web                --logs-dir ./build/logs --api-dsn "$${API_DSN:-https://localhost/}"

## migrate: apply pending SQL migrations
migrate: build
	@set -a; . .env; set +a; ./build/migrator

## build: format, build the WASM bundle (+gzip), then compile all service binaries into ./build/
build: format
	go vet ./...
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" ./cmd/web/static/wasm_exec.js
	CGO_ENABLED=0 GOOS=js GOARCH=wasm go build -o ./cmd/web/static/app.wasm ./cmd/wasm/main.go
	gzip -kfn -9 ./cmd/web/static/app.wasm
	CGO_ENABLED=0 go build -o ./build/collector  -ldflags ${BUILD_OPTIONS} ./cmd/collector/main.go
	CGO_ENABLED=0 go build -o ./build/notifier   -ldflags ${BUILD_OPTIONS} ./cmd/notifier/main.go
	CGO_ENABLED=0 go build -o ./build/migrator   -ldflags ${BUILD_OPTIONS} ./cmd/migrator
	CGO_ENABLED=0 go build -o ./build/web        -ldflags ${BUILD_OPTIONS} ./cmd/web
	CGO_ENABLED=0 go build -o ./build/doctor     -ldflags ${BUILD_OPTIONS} ./cmd/doctor

## doctor-help: print doctor combined usage and subcommand descriptions
doctor-help: build
	./build/doctor --help 2>&1 || true

## audit-help: print doctor audit usage and exit codes
audit-help: build
	./build/doctor audit --help 2>&1 || true



## test: format, go vet, the full race-enabled native suite, and the WASM-runtime tests (WASM skipped with a warning if Node is absent)
test: format
	go clean -cache
	CGO_ENABLED=0 go vet ./...
	CGO_ENABLED=0 go test -race ./...
	@if command -v node >/dev/null 2>&1; then \
		echo "running WASM tests..."; \
		CGO_ENABLED=0 GOOS=js GOARCH=wasm go test \
			-exec "$$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
			./cmd/wasm/dom/... ./cmd/wasm/apiclient/...; \
	else \
		echo "WARNING: 'node' not found — skipping WASM tests (install Node.js 18+ to run them)"; \
	fi

## lint: go vet + forbidden-import guard (no CGO-dependent SQLite driver in go.mod)
lint:
	CGO_ENABLED=0 go vet ./...
	@if grep -qE 'github.com/mattn/go-sqlite3' go.mod; then \
		echo "lint failure: forbidden CGO-dependent SQLite driver in go.mod (use modernc.org/sqlite)"; \
		exit 1; \
	fi

## audit: probe seeded rate sources; default audits all sources; override with ARGS="--source halyk_usd" (exits non-zero on any MISS)
ARGS ?= --all
audit: build
	./build/doctor audit $(ARGS)

## swagger: regenerate Swagger/OpenAPI documentation
swagger:
	swag init -g cmd/web/main.go -o internal/gateway/swagger



## format: run go fmt across all packages
format:
	go fmt ./...



## clean: remove built binaries and generated WASM assets, then go mod tidy
clean:
	rm -f ./build/monitor ./build/collector ./build/notifier ./build/migrator ./build/web ./build/monitor.db
	rm -f ./build/doctor
	rm -f ./cmd/web/static/app.wasm ./cmd/web/static/wasm_exec.js
	rm -rf ./build/static
	go mod tidy


## backups: pull each env's latest DB snapshot + service logs from the host into one per-env archive (./backups/{prime,stage}.<stamp>.tar.gz)
backups:
	@mkdir -p ./backups ./tmp
	@stamp=$$(date -u +%Y%m%d-%H%M%S); \
	for env in prime stage; do \
		tmpdir=./tmp/backups-$$env; \
		rm -rf $$tmpdir; mkdir -p $$tmpdir; \
		latest=$$(ssh be-happy.kz "ls -1t /opt/monitor/backups/$${env}_monitor.*.sqlite 2>/dev/null | head -n1"); \
		if [ -n "$$latest" ]; then \
			echo "[$$env] db:   $$latest"; \
			scp be-happy.kz:$$latest $$tmpdir/; \
		else \
			echo "[$$env] db:   no snapshot in /opt/monitor/backups (has sqlite_dump.sh run on the host yet?)"; \
		fi; \
		if ssh be-happy.kz "test -d /opt/monitor/logs/$$env"; then \
			echo "[$$env] logs: /opt/monitor/logs/$$env"; \
			scp -r be-happy.kz:/opt/monitor/logs/$$env $$tmpdir/logs; \
		else \
			echo "[$$env] logs: /opt/monitor/logs/$$env not present"; \
		fi; \
		if [ -n "$$(ls -A $$tmpdir)" ]; then \
			archive=./backups/$$env.$$stamp.tar.gz; \
			tar -czf $$archive -C $$tmpdir .; \
			echo "[$$env] archive: $$archive"; \
		else \
			echo "[$$env] nothing pulled; no archive created"; \
		fi; \
		rm -rf $$tmpdir; \
	done
