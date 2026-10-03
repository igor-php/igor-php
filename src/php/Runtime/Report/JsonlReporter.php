<?php

namespace IgorPhp\IgorBundle\Runtime\Report;

use IgorPhp\IgorBundle\Runtime\LeakReport;

/**
 * Appends one JSON line per leaking request.
 */
class JsonlReporter implements LeakReporterInterface
{
    public function __construct(private string $path)
    {
    }

    public function report(LeakReport $report): void
    {
        $dir = dirname($this->path);
        if (!is_dir($dir)) {
            @mkdir($dir, 0777, true);
        }

        $line = json_encode($report->toArray(), \JSON_UNESCAPED_SLASHES | \JSON_UNESCAPED_UNICODE | \JSON_PARTIAL_OUTPUT_ON_ERROR);
        @file_put_contents($this->path, $line . "\n", \FILE_APPEND | \LOCK_EX);
    }
}
