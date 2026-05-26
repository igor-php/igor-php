<?php

namespace IgorPhp\IgorBundle\Tests\DataCollector;

use IgorPhp\IgorBundle\DataCollector\IgorDataCollector;
use PHPUnit\Framework\TestCase;
use Symfony\Component\HttpFoundation\Request;
use Symfony\Component\HttpFoundation\Response;

class IgorDataCollectorTest extends TestCase
{
    public function testCollect(): void
    {
        $collector = new IgorDataCollector('/usr/local/bin/igor-php');
        $request = new Request();
        $response = new Response();

        $collector->collect($request, $response);

        $this->assertEquals('igor', $collector->getName());
        $this->assertIsArray($collector->getAuditResults());
    }

    public function testGetAuditResultsFromBinary(): void
    {
        // We'll mock the binary path to a script that returns JSON
        $binaryPath = realpath(__DIR__ . '/../../fixtures/mock-igor.sh');
        $this->assertNotFalse($binaryPath, 'Mock binary not found');
        $collector = new IgorDataCollector($binaryPath);
        
        $collector->collect(new Request(), new Response());
        
        $results = $collector->getAuditResults();
        $this->assertCount(1, $results);
        $this->assertEquals('App\\Service\\LeakService', $results[0]['class']);
    }
}
