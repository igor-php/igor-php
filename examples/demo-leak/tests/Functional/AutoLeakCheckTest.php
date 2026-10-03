<?php

namespace App\Tests\Functional;

use App\Service\StatefulService;
use IgorPhp\IgorBundle\Runtime\Test\AllowRuntimeLeaks;
use IgorPhp\IgorBundle\Runtime\Test\AutoRuntimeLeakCheckTrait;
use PHPUnit\Framework\AssertionFailedError;
use Symfony\Bundle\FrameworkBundle\Test\WebTestCase;

/**
 * No test here calls a leak assertion: the trait checks after each one.
 */
class AutoLeakCheckTest extends WebTestCase
{
    use AutoRuntimeLeakCheckTrait;

    public function testCleanPagesPassWithoutAnyLeakAssertion(): void
    {
        $client = static::createClient();
        $client->request('GET', '/');
        $client->request('GET', '/check-timezone');

        self::assertResponseIsSuccessful();
    }

    #[AllowRuntimeLeaks(reason: 'This experiment leaks on purpose')]
    public function testAKnownLeakCanBeAllowed(): void
    {
        $client = static::createClient();
        $client->request('GET', '/');
        $client->request('GET', '/stateful-service');

        self::assertResponseIsSuccessful();
    }

    #[AllowRuntimeLeaks(reason: 'Calls the check by hand to observe its failure')]
    public function testTheCheckFailsTheTestThatLeaked(): void
    {
        $client = static::createClient();
        $client->request('GET', '/');
        $client->request('GET', '/stateful-service');

        putenv('NO_COLOR=1');
        try {
            $this->assertNoRuntimeLeaks();
            self::fail('The leaking request should have failed the check');
        } catch (AssertionFailedError $failure) {
            self::assertStringContainsString(StatefulService::class, $failure->getMessage());
            self::assertStringContainsString(self::class . '::testTheCheckFailsTheTestThatLeaked', $failure->getMessage());
        } finally {
            putenv('NO_COLOR');
        }
    }
}
