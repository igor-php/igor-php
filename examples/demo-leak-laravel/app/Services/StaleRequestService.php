<?php

namespace App\Services;

use Illuminate\Http\Request;

/**
 * ANTI-PATTERN: Injecting the Request into a singleton.
 * The service will hold onto the FIRST request object forever.
 */
class StaleRequestService
{
    private string $originalPath;
    private float $timestamp;

    public function __construct(
        private Request $request
    ) {
        // We capture these just to show they never change in the demo
        $this->originalPath = $request->path();
        $this->timestamp = microtime(true);
    }

    public function getInfo(): array
    {
        return [
            'captured_path' => $this->originalPath,
            'internal_timestamp' => $this->timestamp,
            // Calling a method on the stale request object
            'object_path' => $this->request->path(),
        ];
    }
}
