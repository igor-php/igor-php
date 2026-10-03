<?php

namespace IgorPhp\IgorBundle\Runtime;

/**
 * Process-wide PHP state that survives between requests in a worker.
 */
class ProcessStateProbe
{
    public const ROOT_ID = '@process';
    public const ROOT_CLASS = 'PHP process state';

    public function read(): array
    {
        return [
            'timezone' => date_default_timezone_get(),
            'cwd' => getcwd(),
            'umask' => sprintf('%04o', umask()),
            'locale' => setlocale(\LC_ALL, '0'),
            'mb_internal_encoding' => function_exists('mb_internal_encoding') ? mb_internal_encoding() : null,
            'gc_enabled' => gc_enabled(),
            'error_reporting' => error_reporting(),
            'ini' => ini_get_all(null, false) ?: [],
            'env' => $_ENV,
        ];
    }
}
