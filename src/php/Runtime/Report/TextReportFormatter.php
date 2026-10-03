<?php

namespace IgorPhp\IgorBundle\Runtime\Report;

use IgorPhp\IgorBundle\Runtime\Finding;
use IgorPhp\IgorBundle\Runtime\LeakReport;
use IgorPhp\IgorBundle\Runtime\SnapshotDiffer;

class TextReportFormatter
{
    private const STYLES = [
        'bold' => '1',
        'dim' => '2',
        'red' => '1;31',
        'green' => '32',
        'yellow' => '1;33',
        'magenta' => '1;35',
        'cyan' => '1;36',
    ];

    private const KIND_STYLES = [
        SnapshotDiffer::GROWTH => 'red',
        SnapshotDiffer::OVERWRITE => 'yellow',
        SnapshotDiffer::INITIALIZED => 'magenta',
    ];

    /**
     * @param bool $decorated adds ANSI colors; keep it off for log files
     */
    public function __construct(private bool $decorated = false)
    {
    }

    /**
     * Colors are on unless the NO_COLOR convention or a dumb terminal says otherwise.
     */
    public static function forTerminal(): self
    {
        return new self(getenv('NO_COLOR') === false && getenv('TERM') !== 'dumb');
    }

    public function isDecorated(): bool
    {
        return $this->decorated;
    }

    public function format(LeakReport $report): string
    {
        $lines = [$this->style('red', '✗ Runtime leak') . ' after ' . $this->describeRequest($report)];
        if (!empty($report->context['label'])) {
            $lines[] = $this->style('dim', '  in ' . $report->context['label']);
        }

        $byService = [];
        foreach ($report->findings as $finding) {
            $byService[$finding->serviceId][] = $finding;
        }

        foreach ($byService as $serviceId => $findings) {
            $class = $findings[0]->class;
            $lines[] = '';
            $lines[] = '  ' . $this->style('cyan', $class)
                . ($serviceId !== $class ? '  ' . $this->style('dim', '(' . $serviceId . ')') : '');

            foreach ($findings as $finding) {
                $lines[] = '    ' . $this->style('bold', $finding->path) . '  ' . $this->badge($finding);
                $lines[] = '      ' . $this->style('dim', $finding->before) . ' → ' . $this->style('bold', $finding->after);
                foreach ($finding->added as $added) {
                    $lines[] = '      ' . $this->style('green', '+ ' . $added);
                }
                $lines[] = '      ' . $this->style('dim', $this->history($finding));
            }
        }

        return implode("\n", $lines);
    }

    /**
     * @param LeakReport[] $reports
     */
    public function formatAll(array $reports): string
    {
        return implode("\n\n", array_map([$this, 'format'], $reports));
    }

    private function describeRequest(LeakReport $report): string
    {
        if (!isset($report->context['method'])) {
            return $this->style('bold', 'no request') . $this->style('dim', '  (state changed between two requests)');
        }

        $text = $this->style('bold', $report->context['method'] . ' ' . $report->context['path']);
        if (!empty($report->context['route'])) {
            $text .= $this->style('dim', '  (route: ' . $report->context['route'] . ')');
        }

        return $text;
    }

    private function badge(Finding $finding): string
    {
        $text = strtoupper($finding->kind);
        if ($finding->kind === SnapshotDiffer::GROWTH) {
            $text .= ' +' . $finding->delta;
        }

        return $this->style(self::KIND_STYLES[$finding->kind] ?? 'bold', $text);
    }

    private function history(Finding $finding): string
    {
        $text = 'seen on ' . $finding->occurrences . '/' . $finding->requestsObserved . ' requests';
        if ($finding->phase !== null) {
            $text .= ' · changed ' . ($finding->phase === 'controller' ? 'in the controller' : $finding->phase);
        }

        return $text;
    }

    private function style(string $style, string $text): string
    {
        return $this->decorated ? "\033[" . self::STYLES[$style] . 'm' . $text . "\033[0m" : $text;
    }
}
