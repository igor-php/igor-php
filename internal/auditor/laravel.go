package auditor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// LaravelSafeNamespaces are the framework namespaces skipped when a Laravel project is detected:
// Octane resets the framework's own state between requests.
var LaravelSafeNamespaces = []string{"Illuminate\\", "Laravel\\"}

// ErrOctaneRequired is returned for a Laravel project without Laravel Octane: igor audits a Laravel
// application as it runs in a FrankenPHP worker, which Octane provides. igor stops on it instead of
// falling back to a directory scan.
var ErrOctaneRequired = errors.New("Laravel Octane is not installed")

// octaneMissingExitCode is the exit code of `php artisan igor:dump` without Octane
// (IgorPhp\Laravel\Console\DumpServiceMapCommand::OCTANE_MISSING).
const octaneMissingExitCode = 3

// octaneMissingError carries the message of igor:dump, which says how to install Octane.
type octaneMissingError struct{ output string }

func (e octaneMissingError) Error() string {
	if e.output == "" {
		return ErrOctaneRequired.Error()
	}
	return e.output
}

func (e octaneMissingError) Unwrap() error { return ErrOctaneRequired }

// DetectLaravel finds a Laravel project (its artisan script) and loads the service map exported
// by `php artisan igor:dump`. It returns nil, nil when the project is not a Laravel project.
func DetectLaravel(rootPath string, cfg config.Config) (*SymfonyBridge, error) {
	projectRoot := rootPath
	if _, err := os.Stat(filepath.Join(projectRoot, "artisan")); err != nil {
		// Fallback for vendor/bin context, like Symfony's bin/console detection
		projectRoot = filepath.Dir(rootPath)
		if _, err := os.Stat(filepath.Join(projectRoot, "artisan")); err != nil {
			return nil, nil
		}
	}

	fmt.Fprintf(os.Stderr, "🔍 Laravel project detected at %s. Initializing Deep Audit...\n", projectRoot)
	b := NewSymfonyBridge(projectRoot, "artisan", cfg)
	b.Framework = FrameworkLaravel
	if err := b.loadLaravelServiceMap(); err != nil {
		return nil, err
	}
	return b, nil
}

// loadLaravelServiceMap exports a fresh service map with artisan, then locates the class files.
func (b *SymfonyBridge) loadLaravelServiceMap() error {
	// Pass the path explicitly: the app may use a custom bootstrap directory
	path := filepath.Join(b.Root, "bootstrap", "cache", "igor_service_map.json")
	cmd := exec.Command("php", filepath.Join(b.Root, b.ConsolePath), "igor:dump", "--path="+path, "--no-ansi", "--no-interaction")
	cmd.Dir = b.Root
	if output, err := cmd.CombinedOutput(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == octaneMissingExitCode {
			return octaneMissingError{output: strings.TrimSpace(string(output))}
		}
		return fmt.Errorf("`php artisan igor:dump` failed: %v\n%s\n👉 make sure IgorPhp\\Laravel\\IgorServiceProvider is registered (package discovery does it when igor-php is installed with Composer)",
			err, strings.TrimSpace(string(output)))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("could not read the Laravel service map: %v", err)
	}
	var container symbol.SymfonyContainer
	if err := json.Unmarshal(data, &container); err != nil {
		return fmt.Errorf("failed to parse the Laravel service map %s: %v", path, err)
	}
	b.Container = &container
	fmt.Fprintf(os.Stderr, "⚡ Igor Agent detected: Using service map from %s\n", path)
	// A service the exporter could not resolve may be missing from the audit: say why
	for _, warning := range container.Warnings {
		fmt.Fprintf(os.Stderr, "⚠️  igor:dump: %s\n", warning)
	}

	return b.locateFilesViaReflection(string(data))
}
