.PHONY: build install start start-worker start-classic stop cc status logs

build:
	docker compose build

install: build
	docker compose run --rm --no-deps php-classic composer install --no-progress

cc:
	rm -f bootstrap/cache/*.php
	rm -f storage/logs/*.log storage/destructor_demo.log

start: install
	docker compose up -d
	@echo "🧟 Igor Laravel Leak Labs are running!"
	@echo "👉 WORKER mode (Octane):  http://localhost:8090"
	@echo "👉 CLASSIC mode: http://localhost:8091"

start-worker: install
	docker compose up -d php-worker
	@echo "🧟 Igor Laravel Leak Lab in WORKER mode (Octane) is running on http://localhost:8090"

start-classic: install
	docker compose up -d php-classic
	@echo "🧟 Igor Laravel Leak Lab in CLASSIC mode is running on http://localhost:8091"

stop:
	docker compose down

status:
	docker compose ps

logs:
	docker compose logs -f
