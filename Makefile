COMPOSE ?= docker compose
TEST_PG_CONTAINER ?= marktrace-test-pg
TEST_PG_PORT ?= 55433

.PHONY: help gen up down reset logs ps test test-go test-web verify demo

help:
	@echo "make gen     - regenerate T1, mock registry, demo codes and golden (seed 16)"
	@echo "make up      - build and start the stack (web http://localhost:8080)"
	@echo "make down    - stop the stack (data kept)"
	@echo "make reset   - stop the stack and drop the database volume"
	@echo "make test    - go tests (backend with throwaway PostgreSQL, mock, gen) + web build"
	@echo "make verify  - fresh stack, import T1, golden compare 100%, re-import, roles, web"
	@echo "make demo    - D16 demo scenario via the API on the running stack"

gen:
	cd tools/gen && go run . -seed 16 -out ../../

up:
	$(COMPOSE) up -d --build
	@echo "Web: http://localhost:8080  API: http://localhost:8081/api  Mock: http://localhost:8082"

down:
	$(COMPOSE) down

reset:
	$(COMPOSE) down -v

logs:
	$(COMPOSE) logs -f --tail=100

ps:
	$(COMPOSE) ps

test: test-go test-web

# Backend integration tests need PostgreSQL: a throwaway container is started and removed.
test-go:
	cd tools/gen && go vet ./... && go test -count=1 ./...
	cd mock && go vet ./... && go test -count=1 ./...
	@docker rm -f $(TEST_PG_CONTAINER) >/dev/null 2>&1 || true
	docker run -d --name $(TEST_PG_CONTAINER) -e POSTGRES_PASSWORD=pg -p 127.0.0.1:$(TEST_PG_PORT):5432 postgres:16-alpine >/dev/null
	@for i in $$(seq 1 60); do docker exec $(TEST_PG_CONTAINER) pg_isready -U postgres >/dev/null 2>&1 && break; sleep 1; done; sleep 1
	cd backend && go vet ./... && \
		TEST_DATABASE_URL='postgres://postgres:pg@127.0.0.1:$(TEST_PG_PORT)/postgres?sslmode=disable' \
		go test -count=1 ./... ; rc=$$?; docker rm -f $(TEST_PG_CONTAINER) >/dev/null; exit $$rc

test-web:
	cd web && npm ci --no-audit --no-fund && npm run build

verify:
	$(COMPOSE) down -v
	$(COMPOSE) up -d --build
	bash scripts/verify.sh

demo:
	bash scripts/demo.sh
