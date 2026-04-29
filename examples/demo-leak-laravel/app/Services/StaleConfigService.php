<?php

namespace App\Services;

use Illuminate\Contracts\Config\Repository;

/**
 * ANTI-PATTERN: Fetching and storing a config value in the constructor.
 * Since this is a singleton, it will NEVER see config changes made after boot.
 */
class StaleConfigService
{
    private string $appName;

    public function __construct(Repository $config)
    {
        // BUG: The value is captured once and stays forever in RAM
        $this->appName = $config->get('app.name');
    }

    public function getAppName(): string
    {
        return $this->appName;
    }
}
