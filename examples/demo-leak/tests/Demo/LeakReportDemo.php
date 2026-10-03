<?php

namespace App\Tests\Demo;

use IgorPhp\IgorBundle\Runtime\Test\RuntimeLeakAssertionsTrait;
use Symfony\Bundle\FrameworkBundle\Test\WebTestCase;

/**
 * Fails on purpose, to show the report a test gets when a request leaves state behind.
 * The file name does not end in "Test.php", so the normal suite skips it. Run it with: make leak-demo
 */
class LeakReportDemo extends WebTestCase
{
    use RuntimeLeakAssertionsTrait;

    private string $timezone;

    protected function setUp(): void
    {
        $this->timezone = date_default_timezone_get();
    }

    protected function tearDown(): void
    {
        date_default_timezone_set($this->timezone);
        parent::tearDown();
    }

    public function testABrowsingSessionThroughTheLab(): void
    {
        $client = $this->createLeakWatchedClient();
        $client->request('GET', '/');

        $client->request('GET', '/stateful-service?tenant=42');
        $client->request('GET', '/stateful-service?tenant=42');
        $client->request('GET', '/incomplete-reset');
        $client->request('GET', '/closure-leak');
        $client->request('GET', '/closure-leak');
        $client->request('GET', '/poison-filters');
        $client->request('GET', '/local-static');
        $client->request('GET', '/poison-timezone');

        $this->assertNoRuntimeLeaks();
    }
}
