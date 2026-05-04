-include .env

SHELL := /bin/bash

ROOT_DIRECTORY := $(shell pwd)
PROJECT_NAME := $(shell basename "$(PWD)")
VERSION := $(shell git describe --tags --always)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
BUILD := $(shell git rev-parse --short HEAD)
TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

BUILD_OPTIONS := "-s -w -X main.BuildVersion=${BRANCH} -X main.BuildTime=${TIME} -X main.BuildHash=${BUILD}"

.PHONY: all claude_task claude_evaluates_project claude_auto_fix_tests run build build-collector build-notifier build-web build-wasm test lint format swagger clean deploy_environment ruledoctor-up ruledoctor-down ruledoctor-pull ruledoctor-test ruledoctor-test-haiku ruledoctor-test-anthropic ruledoctor-test-groq ruledoctor-fetch ruledoctor-run ruledoctor-regenerate-broken ruledoctor-apply ruledoctor-list


deploy_environment:
	scp -r ./configs/nginx.kz_behappy.conf be-happy.kz:/etc/nginx/sites-available/kz.be-happy
	scp -r ./configs/nginx.kz_behappy_balancer_prime.conf be-happy.kz:/etc/nginx/configurations/kz.be-happy.balancer_prime.conf
	scp -r ./configs/nginx.kz_behappy_balancer_stage.conf be-happy.kz:/etc/nginx/configurations/kz.be-happy.balancer_stage.conf
	scp -r ./configs/nginx.kz_behappy_services_prime.conf be-happy.kz:/etc/nginx/configurations/kz.be-happy.services_prime.conf
	scp -r ./configs/nginx.kz_behappy_services_stage.conf be-happy.kz:/etc/nginx/configurations/kz.be-happy.services_stage.conf
	scp -r ./configs/nginx.kz_behappy_common_settings.conf be-happy.kz:/etc/nginx/snippets/kz.be-happy.common_settings.conf
	scp -r ./configs/nginx.kz_behappy_certificates.conf be-happy.kz:/etc/nginx/certificates/kz.be-happy.conf
	scp -r ./configs/certbot.com_lingocrm.sh be-happy.kz:/opt/letsencrypt/kz.be-happy
	scp -r ./configs/srv.prime_monitor.service be-happy.kz:/tmp/service.prime_monitor
	scp -r ./configs/srv.stage_monitor.service be-happy.kz:/tmp/service.stage_monitor
	#ssh -t be-happy.kz "sudo mv /tmp/service.prime_monitor /etc/systemd/system/prime_monitor.service"
	#ssh -t be-happy.kz "sudo mv /tmp/service.stage_monitor /etc/systemd/system/stage_monitor.service"
	ssh be-happy.kz "sudo nginx -s reload"
	echo "done"



claude_dev_test:
	claude ./.claude/logs/task_20260329232102.s02.plan.md --allowedTools "Edit,Write" --system-prompt "$$(cat ./.claude/prompts/developer.md)" 2>&1 | tee -a ./.claude/logs/task_20260329232102.s03.implementation.md

## claude_implement:
claude_implement:
	$(eval TIME_NUM := $(shell date -u +%Y%m%d%H%M%S))
	vi ./.claude/logs/task_$(TIME_NUM).design.md
	echo "Implement this plan. step by step, following the order of tasks and phases. Do not skip any steps. Do not change the task descriptions or acceptance criteria. Do not add any new tasks. Focus on correctness and completeness for each task before moving to the next one. " | tee ./.claude/plans/task_$(TIME_NUM).md
	claude ./.claude/logs/task_$(TIME_NUM).design.md --allowedTools "Read" --system-prompt "$$(cat ./.claude/prompts/architect.md)" 2>&1 | tee -a ./.claude/plans/task_$(TIME_NUM).md
	claude ./.claude/plans/task_$(TIME_NUM).md --allowedTools "Edit,Write" --system-prompt "$$(cat ./.claude/prompts/developer.md)" 2>&1 | tee -a ./.claude/logs/task_$(TIME_NUM).implementation.md

## claude_implement_plan:
claude_implement_plan:
	$(eval TIME_NUM := $(shell date -u +%Y%m%d%H%M%S))
	vi ./.claude/logs/task_$(TIME_NUM).design.md
	echo "Implement this plan. step by step, following the order of tasks and phases. Do not skip any steps. Do not change the task descriptions or acceptance criteria. Do not add any new tasks. Focus on correctness and completeness for each task before moving to the next one. " | tee ./.claude/plans/task_$(TIME_NUM).md
	claude ./.claude/logs/task_$(TIME_NUM).design.md --allowedTools "Read" --system-prompt "$$(cat ./.claude/prompts/architect.md)" 2>&1 | tee -a ./.claude/plans/task_$(TIME_NUM).md

## claude_implement_task:
claude_implement_task:
	claude ./.claude/plans/task_20260412054316.md --allowedTools "Edit,Write" --system-prompt "$$(cat ./.claude/prompts/developer.md)" 2>&1 | tee -a ./.claude/logs/task_$(TIME_NUM).implementation.md

## claude_evaluate:
claude_evaluate:
	$(eval TIME_NUM := $(shell date -u +%Y%m%d%H%M%S))
	claude "you need to evaluate the project. pros and cons." --allowedTools "Read" --system-prompt "$$(cat ./.claude/prompts/architect.md)" 2>&1 | tee -a ./.claude/logs/task_$(TIME_NUM).evaluation.md

claude_reviewer:
	$(eval TIME_NUM := $(shell date -u +%Y%m%d%H%M%S))
	claude "could you review my changes. detaisl: - design: .claude/logs/task_20260414033233.design.md; - plan: .claude/plans/task_20260414033233.md; - implementation: .claude/logs/task_20260414033233.implementation.md" --allowedTools "Read" --system-prompt "$$(cat ./.claude/prompts/reviewer.md)" 2>&1 | tee -a ./.claude/logs/task_$(TIME_NUM).review.md

## claude_auto_fix_tests:
claude_auto_fix_tests:
	$(eval TIME_NUM := $(shell date -u +%Y%m%d%H%M%S))
	go clean -cache
	CGO_ENABLED=0 go vet ./... 2>&1 | tee ./.claude/logs/issues_$(TIME_NUM).s01.logs.md
	CGO_ENABLED=0 go test -race -timeout 5m ./... 2>&1 | tee -a ./.claude/logs/issues_$(TIME_NUM).s01.logs.md
	claude ./.claude/logs/issues_$(TIME_NUM).s01.logs.md --allowedTools "Edit,Write" --system-prompt "$$(cat ./.claude/prompts/tester.md)" 2>&1 | tee -a ./.claude/logs/issues_$(TIME_NUM).s02.fixes.md



## run:
run: build
	@set -a; . .env; set +a; CGO_ENABLED=0 go run -ldflags ${BUILD_OPTIONS} ./cmd/collector/main.go  --logs-dir ./build/logs
	@set -a; . .env; set +a; CGO_ENABLED=0 go run -ldflags ${BUILD_OPTIONS} ./cmd/notifier/main.go   --logs-dir ./build/logs
	#@set -a; . .env; set +a; CGO_ENABLED=0 go run -ldflags ${BUILD_OPTIONS} ./cmd/web                 --logs-dir ./build/logs



## build:
build:
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" ./cmd/web/static/wasm_exec.js
	CGO_ENABLED=0 GOOS=js GOARCH=wasm go build -o ./cmd/web/static/app.wasm ./cmd/wasm/main.go
	CGO_ENABLED=0 go build -o ./build/collector -ldflags ${BUILD_OPTIONS} ./cmd/collector/main.go
	CGO_ENABLED=0 go build -o ./build/notifier  -ldflags ${BUILD_OPTIONS} ./cmd/notifier/main.go
	CGO_ENABLED=0 go build -o ./build/web       -ldflags ${BUILD_OPTIONS} ./cmd/web



## test:
test: format
	go clean -cache
	CGO_ENABLED=0 go vet ./...
	CGO_ENABLED=0 go test -race ./...

## lint: run go vet across all packages
lint: format
	CGO_ENABLED=0 go vet ./...

## swagger: regenerate Swagger/OpenAPI documentation
swagger:
	swag init -g cmd/web/main.go -o internal/gateway/swagger



## format:
format:
	go fmt ./...



## ruledoctor-up: start the local Ollama container used by the ruledoctor hypothesis test
ruledoctor-up:
	docker compose -f docker-compose.ollama.yml up -d

## ruledoctor-down: stop and remove the local Ollama container
ruledoctor-down:
	docker compose -f docker-compose.ollama.yml down

## ruledoctor-pull: pull the default model into the running container (override with MODEL=...)
ruledoctor-pull:
	docker exec fx-ollama-dev ollama pull $(or $(MODEL),qwen2.5:1.5b-instruct)

## ruledoctor-test: run the LLM extraction integration test against the local Ollama
##   MODEL=...  override the model      (default qwen2.5:1.5b-instruct)
##   LIMIT=N    only test first N pairs (default 0 = all 39)
##   TIMEOUT=Xs per-request timeout     (default 180s)
ruledoctor-test:
	RULEDOCTOR_PROVIDER=ollama \
	OLLAMA_URL=http://127.0.0.1:11434 \
	RULEDOCTOR_MODEL=$(or $(MODEL),qwen2.5:1.5b-instruct) \
	RULEDOCTOR_LIMIT=$(or $(LIMIT),0) \
	RULEDOCTOR_TIMEOUT=$(or $(TIMEOUT),180s) \
	RULEDOCTOR_SOURCE=$(SOURCE) \
	CGO_ENABLED=0 go test -v -race -count=1 -timeout 60m -run TestExtract ./internal/ruledoctor/...

## ruledoctor-test-haiku: run extraction test via the local `claude` CLI.
##   Uses whatever auth Claude Code has (subscription / OAuth / API key).
##   MODEL=...  override the model      (default haiku; can be sonnet/opus/full id)
##   EFFORT=... effort level            (default low; medium/high/xhigh/max)
##   LIMIT=N    only test first N pairs (default 0 = all 39)
##   TIMEOUT=Xs per-call timeout        (default 120s)
ruledoctor-test-haiku:
	@command -v claude >/dev/null 2>&1 || { echo "ERROR: 'claude' CLI not found in PATH"; exit 1; }
	RULEDOCTOR_PROVIDER=claudecode \
	RULEDOCTOR_MODEL=$(or $(MODEL),haiku) \
	RULEDOCTOR_EFFORT=$(or $(EFFORT),low) \
	RULEDOCTOR_LIMIT=$(or $(LIMIT),0) \
	RULEDOCTOR_TIMEOUT=$(or $(TIMEOUT),120s) \
	RULEDOCTOR_SOURCE=$(SOURCE) \
	CGO_ENABLED=0 go test -v -race -count=1 -timeout 60m -run TestExtract ./internal/ruledoctor/...

## ruledoctor-test-anthropic: run extraction test directly via Anthropic API (separate billing).
##   Requires ANTHROPIC_API_KEY in env.
ruledoctor-test-anthropic:
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then echo "ERROR: ANTHROPIC_API_KEY is not set"; exit 1; fi
	RULEDOCTOR_PROVIDER=anthropic \
	RULEDOCTOR_MODEL=$(or $(MODEL),claude-haiku-4-5-20251001) \
	RULEDOCTOR_LIMIT=$(or $(LIMIT),0) \
	RULEDOCTOR_TIMEOUT=$(or $(TIMEOUT),60s) \
	RULEDOCTOR_SOURCE=$(SOURCE) \
	CGO_ENABLED=0 go test -v -race -count=1 -timeout 30m -run TestExtract ./internal/ruledoctor/...

## ruledoctor-test-groq: run extraction test against Groq (free tier, ~14400 req/day).
##   Reads GROQ_API_KEY from .env if present, otherwise from the shell.
##   MODEL=...   override model (default llama-3.3-70b-versatile; try llama-3.1-8b-instant for speed)
##   LIMIT=N     only test first N pairs per fixture (default 0 = all)
##   SOURCE=name only test the named fixture (e.g. SOURCE=bcc)
##   TIMEOUT=Xs  per-request timeout (default 60s)
ruledoctor-test-groq:
	@set -a; [ -f .env ] && . .env; set +a; \
	if [ -z "$$GROQ_API_KEY" ]; then echo "ERROR: GROQ_API_KEY is not set (in .env or shell)"; exit 1; fi; \
	GROQ_API_KEY="$$GROQ_API_KEY" \
	RULEDOCTOR_PROVIDER=groq \
	RULEDOCTOR_MODEL=$(or $(MODEL),llama-3.3-70b-versatile) \
	RULEDOCTOR_LIMIT=$(or $(LIMIT),0) \
	RULEDOCTOR_TIMEOUT=$(or $(TIMEOUT),60s) \
	RULEDOCTOR_SOURCE=$(SOURCE) \
	CGO_ENABLED=0 go test -v -race -count=1 -timeout 30m -run TestExtract ./internal/ruledoctor/...

## ruledoctor-fetch: render a URL via headless Chrome (chromedp) and dump the resulting DOM.
##   URL=...   target URL (required)
##   OUT=...   output path for the rendered HTML (default ./tmp/<host>.html)
##   WAIT=...  wait after navigation (default 6s)
ruledoctor-fetch:
	@if [ -z "$(URL)" ]; then echo "ERROR: URL=https://... is required"; exit 1; fi
	$(eval HOST := $(shell echo "$(URL)" | sed -E 's|https?://([^/]+).*|\1|'))
	$(eval OUT  := $(or $(OUT),./tmp/$(HOST).html))
	@mkdir -p $(dir $(OUT))
	CGO_ENABLED=0 go run ./cmd/ruledoctor-fetch \
		-url $(URL) \
		-out $(OUT) \
		-wait $(or $(WAIT),6s)
	@echo "wrote $(OUT)"



## clean:
clean:
	rm -f ./build/monitor ./build/collector ./build/notifier ./build/web ./build/monitor.db
	rm -f ./cmd/web/static/app.wasm ./cmd/web/static/wasm_exec.js
	rm -rf ./build/static
	go mod tidy



## ruledoctor-run: generate extraction rules for TARGET=name1,name2 (requires GROQ_API_KEY or other provider key)
##   TARGET=...    comma-separated target IDs from configs/ruledoctor-targets.json (required)
##   PROVIDER=...  override provider (default groq)
##   MODEL=...     override model
##   OUT_DIR=...   override output directory (default ./tmp/ruledoctor-out/)
ruledoctor-run:
	@if [ -z "$(TARGET)" ]; then echo "ERROR: TARGET=<name>[,<name>] is required"; exit 1; fi
	RULEDOCTOR_PROVIDER=$(or $(PROVIDER),groq) \
	RULEDOCTOR_MODEL=$(or $(MODEL),llama-3.1-8b-instant) \
	RULEDOCTOR_OUT_DIR=$(or $(OUT_DIR),./tmp/ruledoctor-out/) \
	CGO_ENABLED=0 go run ./cmd/ruledoctor generate --target=$(TARGET)

## ruledoctor-regenerate-broken: re-generate rules for all broken targets (reads SQLITEDB_DSN)
ruledoctor-regenerate-broken:
	RULEDOCTOR_PROVIDER=$(or $(PROVIDER),groq) \
	RULEDOCTOR_MODEL=$(or $(MODEL),llama-3.1-8b-instant) \
	RULEDOCTOR_OUT_DIR=$(or $(OUT_DIR),./tmp/ruledoctor-out/) \
	CGO_ENABLED=0 go run ./cmd/ruledoctor regenerate-broken

## ruledoctor-apply: print and apply a SQL artifact FILE=<path> to the SQLite database
##   FILE=...  path to the .sql file produced by ruledoctor-run (required)
ruledoctor-apply:
	@if [ -z "$(FILE)" ]; then echo "ERROR: FILE=<path.sql> is required"; exit 1; fi
	@if [ ! -f "$(FILE)" ]; then echo "ERROR: $(FILE) not found"; exit 1; fi
	@echo "=== SQL artifact to apply ==="
	@cat $(FILE)
	@echo "=== applying to $$(echo $${SQLITEDB_DSN} | sed -E 's,.*/,,') ==="
	sqlite3 "$$(echo $${SQLITEDB_DSN} | sed -E 's,.*/,,')" < "$(FILE)"

## ruledoctor-list: list active extraction rules (reads SQLITEDB_DSN)
##   TARGET=...  optional: filter to a single target ID
ruledoctor-list:
	SQLITEDB_DSN=$${SQLITEDB_DSN} \
	CGO_ENABLED=0 go run ./cmd/ruledoctor list-rules $(if $(TARGET),--target=$(TARGET),)
