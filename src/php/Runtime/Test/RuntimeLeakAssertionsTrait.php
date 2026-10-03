<?php

namespace IgorPhp\IgorBundle\Runtime\Test;

use IgorPhp\IgorBundle\Runtime\Finding;
use IgorPhp\IgorBundle\Runtime\LeakWatcher;
use IgorPhp\IgorBundle\Runtime\Report\TextReportFormatter;
use PHPUnit\Framework\Assert;
use Symfony\Bundle\FrameworkBundle\KernelBrowser;

/**
 * For Symfony WebTestCase classes. Requires igor.runtime_watch.enabled: true in the test environment.
 */
trait RuntimeLeakAssertionsTrait
{
    /**
     * A client whose kernel survives between requests, as it does in a FrankenPHP worker.
     * With the default reboot, every request gets fresh services and no leak can ever be seen.
     */
    protected function createLeakWatchedClient(array $options = [], array $server = []): KernelBrowser
    {
        $client = static::createClient($options, $server);
        $client->disableReboot();

        $name = method_exists($this, 'name') ? $this->name() : $this->getName();
        $this->leakWatcher()->setLabel(static::class . '::' . $name);

        return $client;
    }

    protected function leakWatcher(): LeakWatcher
    {
        return static::getContainer()->get(LeakWatcher::class);
    }

    /**
     * Closes the last request and returns everything found since the client was created.
     *
     * @return Finding[]
     */
    protected function collectRuntimeLeaks(): array
    {
        $watcher = $this->leakWatcher();
        $watcher->flush();

        $findings = [];
        foreach ($watcher->getReports() as $report) {
            foreach ($report->findings as $finding) {
                $findings[] = $finding;
            }
        }

        return $findings;
    }

    protected function assertNoRuntimeLeaks(): void
    {
        $findings = $this->collectRuntimeLeaks();
        if ($findings === []) {
            Assert::assertTrue(true);

            return;
        }

        Assert::fail(count($findings) . ' piece(s) of state survived a request.' . $this->explainRuntimeLeaks());
    }

    /**
     * Asserts that state at $pathContains of a service of class $class survived a request.
     */
    protected function assertRuntimeLeak(string $class, string $pathContains, ?string $kind = null): Finding
    {
        foreach ($this->collectRuntimeLeaks() as $finding) {
            if ($finding->class === $class && str_contains($finding->path, $pathContains) && ($kind === null || $finding->kind === $kind)) {
                Assert::assertTrue(true);

                return $finding;
            }
        }

        Assert::fail(sprintf(
            'Expected a runtime leak on %s at "%s"%s.%s',
            $class,
            $pathContains,
            $kind === null ? '' : ' of kind ' . $kind,
            $this->leakWatcher()->getReports() === [] ? ' Nothing leaked.' : $this->explainRuntimeLeaks()
        ));
    }

    protected function runtimeLeakReport(): string
    {
        return (new TextReportFormatter())->formatAll($this->leakWatcher()->getReports());
    }

    /**
     * PHPUnit escapes color codes inside failure messages. With colors on, the styled report goes
     * straight to the terminal and the message only points at it; otherwise the message carries it.
     */
    private function explainRuntimeLeaks(): string
    {
        $formatter = TextReportFormatter::forTerminal();
        if (!$formatter->isDecorated()) {
            return "\n\n" . $this->runtimeLeakReport() . "\n";
        }

        fwrite(\STDERR, "\n\n" . $formatter->formatAll($this->leakWatcher()->getReports()) . "\n\n");

        return ' See the report printed above.';
    }
}
