<?php

namespace IgorPhp\IgorBundle\Tests\Service;

use IgorPhp\IgorBundle\Service\IgorUsageTracker;
use PHPUnit\Framework\TestCase;

class IgorUsageTrackerTest extends TestCase
{
    public function testMarkAsUsed(): void
    {
        $tracker = new IgorUsageTracker();
        $tracker->markAsUsed('App\Service\MyService');
        $tracker->markAsUsed('App\Service\AnotherService');
        $tracker->markAsUsed('App\Service\MyService'); // Duplicate

        $used = $tracker->getUsedClasses();
        $this->assertCount(2, $used);
        $this->assertContains('App\Service\MyService', $used);
        $this->assertContains('App\Service\AnotherService', $used);
    }

    public function testReset(): void
    {
        $tracker = new IgorUsageTracker();
        $tracker->markAsUsed('App\Service\MyService');
        
        $tracker->reset();
        
        $this->assertEmpty($tracker->getUsedClasses());
    }
}
