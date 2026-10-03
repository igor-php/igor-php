<?php

namespace App\Tests\Functional;

use App\Service\ClosureLeakService;
use App\Service\FakeEntityManager;
use App\Service\IncompleteResetService;
use App\Service\LocalStaticService;
use App\Service\StatefulService;
use App\Service\StaticLeakService;
use IgorPhp\IgorBundle\Runtime\ProcessStateProbe;
use IgorPhp\IgorBundle\Runtime\SnapshotDiffer;
use IgorPhp\IgorBundle\Runtime\Test\RuntimeLeakAssertionsTrait;
use PHPUnit\Framework\Attributes\DataProvider;
use Symfony\Bundle\FrameworkBundle\KernelBrowser;
use Symfony\Bundle\FrameworkBundle\Test\WebTestCase;

/**
 * Runs the Leak Lab experiments against a kernel that survives between requests, like a
 * FrankenPHP worker, and checks that the runtime watcher reports each leak.
 */
class LeakLabRuntimeTest extends WebTestCase
{
    use RuntimeLeakAssertionsTrait;

    private array $processState;

    protected function setUp(): void
    {
        $this->processState = [date_default_timezone_get(), getcwd(), umask(), mb_internal_encoding(), gc_enabled()];
        @unlink($this->jsonlPath());
    }

    protected function tearDown(): void
    {
        // The lab poisons the real PHP process: put it back for the next test
        [$timezone, $cwd, $umask, $encoding, $gc] = $this->processState;
        date_default_timezone_set($timezone);
        chdir($cwd);
        umask($umask);
        mb_internal_encoding($encoding);
        $gc ? gc_enable() : gc_disable();

        parent::tearDown();
    }

    /**
     * A first request to the lab's home page instantiates the controller and its services, so the
     * watcher has a clean "before" for them. A service first created during a request has none.
     */
    private function warmedUpClient(): KernelBrowser
    {
        $client = $this->createLeakWatchedClient();
        $client->request('GET', '/');
        self::assertResponseIsSuccessful();

        return $client;
    }

    public function testCleanPageLeavesNothingBehind(): void
    {
        $client = $this->warmedUpClient();
        $client->request('GET', '/');
        $client->request('GET', '/check-timezone');

        $this->assertNoRuntimeLeaks();
    }

    public function testStatefulServiceKeepsGrowing(): void
    {
        $client = $this->warmedUpClient();
        $client->request('GET', '/stateful-service');
        $client->request('GET', '/stateful-service');
        $client->request('GET', '/stateful-service');

        $finding = $this->assertRuntimeLeak(StatefulService::class, '->cache', SnapshotDiffer::GROWTH);

        self::assertSame(1, $finding->delta);
        self::assertSame('controller', $finding->phase);

        $report = $this->runtimeLeakReport();
        self::assertStringContainsString('GET /stateful-service', $report);
        self::assertStringContainsString(self::class . '::testStatefulServiceKeepsGrowing', $report);
        self::assertStringContainsString('seen on 3/', $report);
    }

    public function testIncompleteResetOnlyReportsTheForgottenProperty(): void
    {
        $client = $this->warmedUpClient();
        $client->request('GET', '/incomplete-reset');

        $paths = array_map(static fn ($f) => $f->path, array_filter(
            $this->collectRuntimeLeaks(),
            static fn ($f) => $f->class === IncompleteResetService::class
        ));

        // reset() empties $clean, so only $forgotten survives the request
        self::assertSame(['->forgotten'], array_values($paths));
    }

    public function testStaticPropertyLeak(): void
    {
        $client = $this->warmedUpClient();
        $client->request('GET', '/static-leak');

        $this->assertRuntimeLeak(StaticLeakService::class, '::$hist', SnapshotDiffer::GROWTH);
    }

    public function testMethodLocalStaticVariableLeak(): void
    {
        $client = $this->warmedUpClient();
        $client->request('GET', '/local-static');

        $this->assertRuntimeLeak(LocalStaticService::class, '::incrementAndGet() static $counter', SnapshotDiffer::OVERWRITE);
    }

    public function testClosureLeakPointsAtWhereTheClosureWasDefined(): void
    {
        $client = $this->warmedUpClient();
        $client->request('GET', '/closure-leak');
        $client->request('GET', '/closure-leak');

        $finding = $this->assertRuntimeLeak(ClosureLeakService::class, '->listeners[request]', SnapshotDiffer::GROWTH);

        self::assertStringContainsString('LeakDemoController.php:', $finding->added[0]);
        self::assertStringContainsString('use ($v)', $finding->added[0]);
    }

    public function testMutationInsideANestedObjectOfASharedService(): void
    {
        $client = $this->warmedUpClient();
        $client->request('GET', '/poison-filters');

        // FakeEntityManager->filters is the same object before and after: only its content changed
        $this->assertRuntimeLeak(FakeEntityManager::class, '->filters->disabledFilters', SnapshotDiffer::GROWTH);
    }

    public function testTimezonePoisoning(): void
    {
        date_default_timezone_set('UTC');
        $client = $this->warmedUpClient();
        $client->request('GET', '/poison-timezone');

        $finding = $this->assertRuntimeLeak(ProcessStateProbe::ROOT_CLASS, '->timezone', SnapshotDiffer::OVERWRITE);

        self::assertSame("'UTC'", $finding->before);
        self::assertSame("'America/New_York'", $finding->after);
    }

    public static function processPoisons(): array
    {
        return [
            'umask' => ['umask', '->umask'],
            'encoding' => ['encoding', '->mb_internal_encoding'],
            'gc' => ['gc', '->gc_enabled'],
            'cwd' => ['cwd', '->cwd'],
        ];
    }

    #[DataProvider('processPoisons')]
    public function testProcessStatePoisoning(string $poison, string $path): void
    {
        $client = $this->warmedUpClient();
        $client->request('GET', '/process-state-leak?poison=' . $poison);

        $this->assertRuntimeLeak(ProcessStateProbe::ROOT_CLASS, $path, SnapshotDiffer::OVERWRITE);
    }

    public function testEachLeakingRequestIsAppendedToTheJsonlLog(): void
    {
        $client = $this->warmedUpClient();
        $client->request('GET', '/stateful-service?tenant=42');
        $this->collectRuntimeLeaks();

        $lines = file($this->jsonlPath(), \FILE_IGNORE_NEW_LINES);
        self::assertCount(1, $lines);

        $entry = json_decode($lines[0], true);
        self::assertSame('/stateful-service', $entry['path']);
        self::assertSame(['tenant' => 'string'], $entry['payload_shape']['query'], 'Only the shape of the payload is logged, never its values');
        self::assertSame(StatefulService::class, $entry['findings'][0]['class']);
        self::assertSame('growth', $entry['findings'][0]['kind']);
    }

    public function testWithoutAWarmUpTheFirstRequestIsOnlyABaseline(): void
    {
        $client = $this->createLeakWatchedClient();
        $client->request('GET', '/stateful-service');

        // The services were created during this very request: there is no "before" to compare with
        self::assertSame([], $this->collectRuntimeLeaks());

        $client->request('GET', '/stateful-service');
        $this->assertRuntimeLeak(StatefulService::class, '->cache', SnapshotDiffer::GROWTH);
    }

    private function jsonlPath(): string
    {
        return dirname(__DIR__, 2) . '/var/log/igor-leaks.jsonl';
    }
}
