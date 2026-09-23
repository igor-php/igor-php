<?php

namespace App\Service;

class ProcessStateLeakService
{
    public function getCurrentState(): array
    {
        return [
            'cwd' => getcwd(),
            'umask' => sprintf('%04o', umask()),
            'mb_internal_encoding' => mb_internal_encoding(),
            'gc_enabled' => gc_enabled() ? 'ENABLED' : 'DISABLED',
        ];
    }

    public function poisonCwd(string $dir = '/tmp'): void
    {
        chdir($dir);
    }

    public function poisonUmask(int $mask = 0077): void
    {
        umask($mask);
    }

    public function poisonEncoding(string $encoding = 'ISO-8859-1'): void
    {
        mb_internal_encoding($encoding);
    }

    public function poisonGc(): void
    {
        gc_disable();
    }
}
