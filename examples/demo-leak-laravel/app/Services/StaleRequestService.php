<?php

namespace App\Services;

use Illuminate\Http\Request;

class StaleRequestService
{
    // Captured once, when the container builds this singleton
    public function __construct(private Request $request) {}

    public function describe(): array
    {
        return [
            'url' => $this->request->fullUrl(),
            'name' => $this->request->query('name', '(none)'),
        ];
    }
}
