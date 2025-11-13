SERVICE_DIR := services/cloud-access-core
BIN := cloud-access-core
RTC_SERVICE_DIR := services/realtime-interview-core
RTC_BIN := realtime-interview-core
DG_CLIENT_DIR := clients/desktop-guardian-client
BIN_DIR := bin
COMPOSE_FILE := docker/docker-compose.cloud-access-core.yml
ENV_FILE := configs/.env
COMPOSE := docker compose

.PHONY: run test tidy fmt build compose-up compose-down compose-logs compose-restart \
	rtc-run rtc-test rtc-test-integration rtc-fmt rtc-tidy rtc-build \
	guardian-install guardian-dev guardian-lint guardian-test guardian-build

run:
	cd $(SERVICE_DIR) && CLOUD_ACCESS_CONFIG=../../configs/config.example.yaml go run ./cmd/$(BIN)

test:
	cd $(SERVICE_DIR) && go test ./...

test-integration:
	cd $(SERVICE_DIR) && go test -tags=integration ./tests/...

fmt:
	cd $(SERVICE_DIR) && gofmt -w $$(find . -name '*.go')

tidy:
	cd $(SERVICE_DIR) && go mod tidy

build:
	mkdir -p $(BIN_DIR)
	cd $(SERVICE_DIR) && go build -o ../../$(BIN_DIR)/$(BIN) ./cmd/$(BIN)

rtc-run:
	cd $(RTC_SERVICE_DIR) && REALTIME_INTERVIEW_CONFIG=./configs/config.example.yaml go run ./cmd/$(RTC_BIN)

rtc-test:
	cd $(RTC_SERVICE_DIR) && go test ./...

rtc-test-integration:
	cd $(RTC_SERVICE_DIR) && go test -tags=integration ./tests/...

rtc-fmt:
	cd $(RTC_SERVICE_DIR) && gofmt -w $$(find . -name '*.go')

rtc-tidy:
	cd $(RTC_SERVICE_DIR) && go mod tidy

rtc-build:
	mkdir -p $(BIN_DIR)
	cd $(RTC_SERVICE_DIR) && go build -o ../../$(BIN_DIR)/$(RTC_BIN) ./cmd/$(RTC_BIN)

guardian-install:
	cd $(DG_CLIENT_DIR) && corepack pnpm install

guardian-dev:
	cd $(DG_CLIENT_DIR) && corepack pnpm dev

guardian-lint:
	cd $(DG_CLIENT_DIR) && corepack pnpm lint

guardian-test:
	cd $(DG_CLIENT_DIR) && corepack pnpm test

guardian-build:
	cd $(DG_CLIENT_DIR) && corepack pnpm build

guardian-e2e:
	cd $(DG_CLIENT_DIR) && corepack pnpm e2e

compose-up:
	$(COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) up -d --build

compose-down:
	$(COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) down --remove-orphans

compose-logs:
	$(COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) logs -f $(BIN)

compose-restart: compose-down compose-up
