.PHONY: install start start-worker start-classic stop cc status logs test leak-demo

install:
	composer install --no-progress

cc:
	rm -rf var/cache/*
	rm -rf var/log/*

start: install
	docker compose up -d
	@echo "🧟 Igor Leak Labs are running!"
	@echo "👉 WORKER mode:  http://localhost:8080"
	@echo "👉 CLASSIC mode: http://localhost:8081"

start-worker: install
	docker compose up -d php-worker
	@echo "🧟 Igor Leak Lab in WORKER mode is running on http://localhost:8080"

start-classic: install
	docker compose up -d php-classic
	@echo "🧟 Igor Leak Lab in CLASSIC mode is running on http://localhost:8081"

stop:
	docker compose down

status:
	docker compose ps

logs:
	docker compose logs -f

# Runs the runtime leak watcher test suite in Docker (the repo root is mounted so ../../src/php resolves)
test:
	docker run --rm -v $(abspath ../..):/repo -w /repo/examples/demo-leak composer:2 \
		sh -c "composer install --no-progress --no-interaction && php bin/phpunit"

# Runs a test that fails on purpose, to show the report produced when requests leave state behind
leak-demo:
	-docker run --rm -v $(abspath ../..):/repo -w /repo/examples/demo-leak composer:2 \
		sh -c "composer install --no-progress --no-interaction -q && php bin/phpunit tests/Demo/LeakReportDemo.php"
