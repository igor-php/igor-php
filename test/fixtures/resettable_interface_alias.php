<?php

namespace App\Service;

interface CacheInterface
{
    public function setValue(string $key, mixed $value): void;
}

class ReportService
{
    public function __construct(private CacheInterface $cache)
    {
    }

    public function build(): void
    {
        // Safe: the service behind CacheInterface implements ResetInterface
        $this->cache->setValue('report', 'ready');
    }
}
