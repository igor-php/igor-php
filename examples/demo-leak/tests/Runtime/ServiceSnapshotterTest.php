<?php

namespace App\Tests\Runtime;

use IgorPhp\IgorBundle\Runtime\ServiceSnapshotter;
use IgorPhp\IgorBundle\Runtime\SnapshotDiffer;
use PHPUnit\Framework\TestCase;

require_once __DIR__ . '/Fixtures.php';

/**
 * Shows what the snapshot walker sees, and what it refuses to touch.
 */
class ServiceSnapshotterTest extends TestCase
{
    private ServiceSnapshotter $snapshotter;
    private SnapshotDiffer $differ;

    protected function setUp(): void
    {
        $this->snapshotter = new ServiceSnapshotter();
        $this->differ = new SnapshotDiffer();
        Cart::forget();
    }

    private function changes(object $root, callable $mutation, array $serviceMap = []): array
    {
        $before = $this->snapshotter->snapshot($root, $serviceMap);
        $mutation();
        $after = $this->snapshotter->snapshot($root, $serviceMap);

        return array_column($this->differ->diff($before, $after), null, 'path');
    }

    public function testUnchangedServiceGivesNoFinding(): void
    {
        $cart = new Cart();

        self::assertSame([], $this->changes($cart, static fn () => null));
    }

    public function testMutationThreeObjectsDeepIsSeenAlthoughObjectIdsAreUnchanged(): void
    {
        $cart = new Cart();
        $addressId = spl_object_id($cart->owner->address);

        $changes = $this->changes($cart, static function () use ($cart) {
            $cart->owner->address->city = 'Lyon';
        });

        self::assertSame($addressId, spl_object_id($cart->owner->address));
        self::assertSame(['->owner->address->city'], array_keys($changes));
        self::assertSame(SnapshotDiffer::OVERWRITE, $changes['->owner->address->city']['kind']);
        self::assertSame("'Paris'", $changes['->owner->address->city']['before']);
        self::assertSame("'Lyon'", $changes['->owner->address->city']['after']);
    }

    public function testGrowingArrayIsReportedOnceAsGrowth(): void
    {
        $cart = new Cart();

        $changes = $this->changes($cart, static function () use ($cart) {
            $cart->items[] = 'a';
            $cart->items[] = 'b';
        });

        self::assertSame(['->items'], array_keys($changes));
        self::assertSame(SnapshotDiffer::GROWTH, $changes['->items']['kind']);
        self::assertSame(2, $changes['->items']['delta']);
    }

    public function testObjectReplacedByAnEqualOneIsNotALeak(): void
    {
        $cart = new Cart();

        $changes = $this->changes($cart, static function () use ($cart) {
            $cart->owner = new Owner();
        });

        self::assertSame([], $changes);
    }

    public function testCyclesAreRecordedNotFollowed(): void
    {
        $cart = new Cart();
        $cart->owner->cart = $cart;

        $tree = $this->snapshotter->snapshot($cart);

        self::assertSame(['@t' => 'ref', 'to' => '(root)'], $tree['props']['owner']['props']['cart']);
    }

    public function testClosureCapturedVariablesAreCompared(): void
    {
        $cart = new Cart();
        $captured = 'first';
        $cart->callback = static fn () => $captured;

        $changes = $this->changes($cart, static function () use ($cart) {
            $captured = 'second';
            $cart->callback = static fn () => $captured;
        });

        // Two different closures: the definition site changed
        self::assertArrayHasKey('->callback', $changes);
        self::assertStringContainsString('ServiceSnapshotterTest.php', $changes['->callback']['after']);
        self::assertStringContainsString('use ($captured)', $changes['->callback']['after']);
    }

    public function testStaticPropertiesAndMethodStaticsAreIncluded(): void
    {
        $cart = new Cart();
        $cart->nextNumber();

        $changes = $this->changes($cart, static function () use ($cart) {
            $cart->remember('x');
            $cart->nextNumber();
        });

        self::assertSame(SnapshotDiffer::GROWTH, $changes['::$history']['kind']);
        self::assertSame('1', $changes['::nextNumber() static $number']['before']);
        self::assertSame('2', $changes['::nextNumber() static $number']['after']);
    }

    public function testWorkerSafePropertyIsIgnored(): void
    {
        $cart = new Cart();

        $changes = $this->changes($cart, static function () use ($cart) {
            $cart->scratch[] = 'anything';
        });

        self::assertSame([], $changes);
    }

    public function testWalkStopsAtAnotherService(): void
    {
        $cart = new Cart();
        $logger = new Owner();
        $cart->collaborator = $logger;
        $serviceMap = [spl_object_id($logger) => 'app.logger'];

        $changes = $this->changes($cart, static function () use ($logger) {
            $logger->address->city = 'Lyon';
        }, $serviceMap);

        // The other service is checked as its own root, not through this one
        self::assertSame([], $changes);
        self::assertSame(
            ['@t' => 'service', 'id' => 'app.logger'],
            $this->snapshotter->snapshot($cart, $serviceMap)['props']['collaborator']
        );
    }

    public function testLazyObjectIsNotWokenUpAndItsLoadIsReported(): void
    {
        $loads = 0;
        $lazy = (new \ReflectionClass(Heavy::class))->newLazyGhost(static function (Heavy $heavy) use (&$loads) {
            ++$loads;
            $heavy->__construct();
        });

        $cart = new Cart();
        $cart->collaborator = $lazy;

        $changes = $this->changes($cart, static fn () => null);
        self::assertSame([], $changes);
        self::assertSame(0, $loads, 'Taking snapshots must never initialize a lazy object');

        $changes = $this->changes($cart, static function () use ($lazy) {
            $lazy->payload;
        });

        self::assertSame(1, $loads);
        self::assertSame(SnapshotDiffer::INITIALIZED, $changes['->collaborator']['kind']);
    }

    public function testNamespacesCanBeMadeOpaque(): void
    {
        $snapshotter = new ServiceSnapshotter(['App\\Tests\\Runtime\\Owner']);
        $cart = new Cart();

        self::assertSame(['@t' => 'opaque', 'desc' => Owner::class], $snapshotter->snapshot($cart)['props']['owner']);
    }

    public function testDepthIsCapped(): void
    {
        $snapshotter = new ServiceSnapshotter([], 1);
        $cart = new Cart();

        self::assertSame(['@t' => 'truncated'], $snapshotter->snapshot($cart)['props']['owner']['props']['address']);
    }
}
