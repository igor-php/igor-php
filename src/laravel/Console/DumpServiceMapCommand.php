<?php

namespace IgorPhp\Laravel\Console;

use IgorPhp\Laravel\ServiceMapBuilder;
use Illuminate\Console\Command;

class DumpServiceMapCommand extends Command
{
    protected $signature = 'igor:dump {--path= : Where to write the map (default: bootstrap/cache/igor_service_map.json)}';

    protected $description = 'Export the service map igor-php uses to audit the services that persist in Octane workers';

    public function handle(): int
    {
        $path = $this->option('path') ?: $this->laravel->bootstrapPath('cache' . DIRECTORY_SEPARATOR . 'igor_service_map.json');

        $builder = new ServiceMapBuilder($this->laravel);
        $map = $builder->build();
        foreach ($builder->getWarnings() as $warning) {
            $this->warn($warning);
        }

        $dir = dirname($path);
        if (!is_dir($dir)) {
            mkdir($dir, 0777, true);
        }
        file_put_contents($path, json_encode($map, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES));

        $this->info(sprintf('Igor service map written to %s (%d definitions).', $path, count($map['definitions'])));

        return self::SUCCESS;
    }
}
