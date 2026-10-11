<?php

namespace App\Listeners;

use App\Services\IncompleteResetService;

class ResetIncompleteResetService
{
    /**
     * Octane equivalent of Symfony's kernel.reset: called on every RequestReceived event.
     */
    public function handle($event): void
    {
        $event->sandbox->make(IncompleteResetService::class)->reset();
    }
}
