<?php

namespace IgorPhp\IgorBundle\Runtime\Test;

use IgorPhp\IgorBundle\Runtime\LeakWatcher;
use PHPUnit\Framework\Assert;
use PHPUnit\Framework\Attributes\Before;
use PHPUnit\Framework\Attributes\PostCondition;
use Symfony\Bundle\FrameworkBundle\KernelBrowser;

/**
 * Checks every test of the class without any call inside the tests: put it on your base
 * functional test case. Requires igor.runtime_watch.enabled: true in the test environment.
 *
 * Opt a test out with #[AllowRuntimeLeaks].
 */
trait AutoRuntimeLeakCheckTrait
{
    use RuntimeLeakAssertionsTrait;

    private static bool $leakWatchedClientCreated = false;

    /**
     * The default client reboots the kernel between two requests, which gives every request fresh
     * services and hides every leak. This one keeps it alive, like a worker does.
     */
    protected static function createClient(array $options = [], array $server = []): KernelBrowser
    {
        $client = parent::createClient($options, $server);
        $client->disableReboot();
        self::$leakWatchedClientCreated = true;

        return $client;
    }

    #[Before]
    protected function prepareRuntimeLeakCheck(): void
    {
        self::$leakWatchedClientCreated = false;

        $name = method_exists($this, 'name') ? $this->name() : $this->getName();
        LeakWatcher::$defaultLabel = static::class . '::' . $name;
    }

    /**
     * Runs right after the test method, while the kernel and its services are still alive.
     */
    #[PostCondition]
    protected function checkRuntimeLeaksAfterTest(): void
    {
        LeakWatcher::$defaultLabel = null;

        if (!static::$booted || !static::getContainer()->has(LeakWatcher::class) || $this->runtimeLeaksAllowed()) {
            return;
        }

        if (!self::$leakWatchedClientCreated && $this->leakWatcher()->getRequestCount() > 0) {
            Assert::fail(
                'Runtime leak check: this test made requests with a client that was not created by '
                . 'AutoRuntimeLeakCheckTrait::createClient(), so its kernel reboots between requests and no leak can be seen. '
                . 'If ' . static::class . ' overrides createClient(), call disableReboot() on the client there.'
            );
        }

        $this->assertNoRuntimeLeaks();
    }

    private function runtimeLeaksAllowed(): bool
    {
        $class = new \ReflectionClass($this);
        if ($class->getAttributes(AllowRuntimeLeaks::class) !== []) {
            return true;
        }

        $name = method_exists($this, 'name') ? $this->name() : $this->getName(false);

        return $class->hasMethod($name) && $class->getMethod($name)->getAttributes(AllowRuntimeLeaks::class) !== [];
    }
}
