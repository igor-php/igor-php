<?php

namespace App\Services;

use Illuminate\Contracts\Config\Repository as Config;
use Illuminate\Contracts\Foundation\Application;
use Illuminate\Http\Request;
use Symfony\Component\HttpFoundation\RequestStack;

// Detected: a persistent service keeps the request it was built with
class PromotedRequest
{
    public function __construct(private readonly Request $request) {}
}

// Detected: typed properties, nullable and aliased types, the Symfony request
class TypedProperties
{
    private ?\Symfony\Component\HttpFoundation\Request $current = null;
    protected Config $config;
    public Application|null $app;

    public function __construct(Config $config)
    {
        $this->config = $config;
    }
}

// Ignored: resolving the request when needed, or keeping the RequestStack
class ResolvesWhenNeeded
{
    public function __construct(private RequestStack $stack) {}

    public function url(): string
    {
        return request()->fullUrl();
    }

    public function handle(Request $request): string
    {
        return $request->path();
    }
}

// Ignored: the request is only read in the constructor
class ReadsInConstructor
{
    private string $locale;

    public function __construct(Request $request)
    {
        $this->locale = $request->getLocale();
    }
}

// Ignored: explicitly marked as safe
class Annotated
{
    public function __construct(
        // @igor-ignore
        private Request $request,
    ) {}
}

// Ignored: a non-shared service is built for every request
class PerRequestService
{
    public function __construct(private Request $request) {}
}
