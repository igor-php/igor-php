<?php

namespace App\Tests\Demo;

use IgorPhp\IgorBundle\Runtime\Test\AutoRuntimeLeakCheckTrait;
use Symfony\Bundle\FrameworkBundle\Test\WebTestCase;

/**
 * Fails on purpose, to show the report a test gets when a request leaves state behind.
 * The test itself asserts nothing about leaks: the trait checks after every test.
 * The file name does not end in "Test.php", so the normal suite skips it. Run it with: make leak-demo
 */
class LeakReportDemo extends WebTestCase
{
    use AutoRuntimeLeakCheckTrait;

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
        $client = static::createClient();
        $client->request('GET', '/');

        $client->request('GET', '/stateful-service?tenant=42');
        $client->request('GET', '/stateful-service?tenant=42');
        $client->request('GET', '/incomplete-reset');
        $client->request('GET', '/closure-leak');
        $client->request('GET', '/closure-leak');
        $client->request('GET', '/poison-filters');
        $client->request('GET', '/local-static');
        $client->request('GET', '/poison-timezone');

        self::assertResponseIsSuccessful();
    }
}
