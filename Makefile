.PHONY: test test-go test-symfony test-laravel setup-labs help

# Default target
test: test-go test-symfony test-laravel

## test-go: Run Go unit tests
test-go:
	@echo "🧪 Running Go unit tests..."
	go test -v ./...

## test-symfony: Setup and audit the Symfony lab
test-symfony:
	@echo "🐘 Auditing Symfony Lab..."
	cd examples/demo-leak-symfony && composer install --quiet
	cd examples/demo-leak-symfony && php bin/console cache:clear --quiet
	-go run . examples/demo-leak-symfony/

## test-laravel: Setup and audit the Laravel lab
test-laravel:
	@echo "🏎️  Auditing Laravel Lab..."
	cd examples/demo-leak-laravel && composer install --quiet
	-go run . examples/demo-leak-laravel/

## setup-labs: Reinstall dependencies and warm up caches
setup-labs:
	@echo "📦 Setting up laboratories..."
	cd examples/demo-leak-symfony && composer install --quiet
	cd examples/demo-leak-symfony && php bin/console cache:clear --quiet
	cd examples/demo-leak-laravel && composer install --quiet
	cd examples/demo-leak-laravel && php artisan key:generate --quiet

## help: Show this help message
help:
	@echo "🧟 Igor-PHP Development Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^##' Makefile | sed -e 's/## //g' | column -t -s ':'
