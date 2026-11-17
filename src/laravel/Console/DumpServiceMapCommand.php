<?php

namespace IgorPhp\Laravel\Console;

use IgorPhp\Laravel\ServiceMapBuilder;
use Illuminate\Console\Command;

class DumpServiceMapCommand extends Command
{
    /** Exit code when Laravel Octane is missing: igor stops instead of falling back to a directory scan. */
    public const OCTANE_MISSING = 3;

    protected $signature = 'igor:dump {--path= : Where to write the map (default: bootstrap/cache/igor_service_map.json)}';

    protected $description = 'Export the service map igor-php uses to audit the services that persist in Octane workers';

    public function handle(): int
    {
        $path = $this->option('path') ?: $this->laravel->bootstrapPath('cache' . DIRECTORY_SEPARATOR . 'igor_service_map.json');

        // Laravel runs in a FrankenPHP worker through Octane: what persists depends on Octane's
        // listeners and on config/octane.php, so the map is only built for an Octane application
        if (!ServiceMapBuilder::octaneInstalled()) {
            $this->error('Laravel Octane is not installed. Igor audits a Laravel application as it runs in a FrankenPHP worker, under Octane.');
            $this->line('Install it first, then run igor again:');
            $this->line('  composer require laravel/octane');
            $this->line('  php artisan octane:install --server=frankenphp');

            return self::OCTANE_MISSING;
        }

        $builder = new ServiceMapBuilder($this->laravel);
        $map = $builder->build();
        foreach ($builder->getWarnings() as $warning) {
            $this->warn($warning);
        }

        // igor must not audit a stale map: fail when the fresh one cannot be written
        $json = json_encode($map, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE);
        $dir = dirname($path);
        if ($json === false || (!is_dir($dir) && !@mkdir($dir, 0777, true) && !is_dir($dir)) || @file_put_contents($path, $json) === false) {
            $this->error(sprintf('Could not write the Igor service map to %s.', $path));

            return self::FAILURE;
        }

        $this->info(sprintf('Igor service map written to %s (%d definitions).', $path, count($map['definitions'])));

        return self::SUCCESS;
    }
}
