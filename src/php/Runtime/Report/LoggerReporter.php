<?php

namespace IgorPhp\IgorBundle\Runtime\Report;

use IgorPhp\IgorBundle\Runtime\LeakReport;

/**
 * Sends each report to the application's own logger, when it has one.
 */
class LoggerReporter implements LeakReporterInterface
{
    public function __construct(
        private TextReportFormatter $formatter,
        private ?object $logger = null,
    ) {
    }

    public function report(LeakReport $report): void
    {
        if ($this->logger === null || !method_exists($this->logger, 'warning')) {
            return;
        }

        $this->logger->warning($this->formatter->format($report), ['igor_leak' => $report->toArray()]);
    }
}
