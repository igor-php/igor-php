<?php

namespace IgorPhp\IgorBundle\DataCollector;

use Symfony\Component\DependencyInjection\ContainerInterface;
use Symfony\Component\HttpFoundation\Request;
use Symfony\Component\HttpFoundation\Response;
use Symfony\Component\HttpKernel\DataCollector\DataCollector;
use Symfony\Component\Process\Process;

class IgorDataCollector extends DataCollector
{
    private string $igorBinaryPath;
    private ContainerInterface $container;

    public function __construct(string $igorBinaryPath, ContainerInterface $container)
    {
        $this->igorBinaryPath = $igorBinaryPath;
        $this->container = $container;
    }

    public function collect(Request $request, Response $response, \Throwable $exception = null): void
    {
        $auditResults = [];
        $rawCount = 0;
        $error = '';
        $binaryFound = file_exists($this->igorBinaryPath);
        $configFound = false;
        $command = '';
        $cwd = '';
        $configPath = '';

        if ($binaryFound) {
            // 1. Identify services used in this request
            $initializedServices = [];
            foreach ($this->container->getServiceIds() as $id) {
                if ($this->container->initialized($id)) {
                    $initializedServices[$id] = true;
                }
            }

            // Get project root (assuming vendor/bin/igor-php)
            $projectDir = dirname($this->igorBinaryPath, 3);
            $cwd = (string) $projectDir;
            $configPath = $projectDir . '/igor.json';
            $configFound = file_exists($configPath);
            
            // IMPORTANT: Use realpath to resolve any symlinks and ensure we have the absolute path to the binary
            $realBinaryPath = realpath($this->igorBinaryPath) ?: $this->igorBinaryPath;

            $args = [$realBinaryPath, '--output', 'json'];
            if ($configFound) {
                $args[] = '--config';
                $args[] = $configPath;
            }
            $args[] = '.';
            $command = implode(' ', $args);

            // Run process from project root
            $process = new Process($args, $projectDir);
            $process->setTimeout(60);
            $process->run();

            $exitCode = $process->getExitCode();
            $output = (string) $process->getOutput();
            $errorOutput = (string) $process->getErrorOutput();

            if ($exitCode === 0 || $exitCode === 1) {
                if (preg_match('/\[.*\]/s', $output, $matches)) {
                    $results = json_decode($matches[0], true) ?: [];
                    
                    $auditResults = [];
                    foreach ($results as $res) {
                        if (empty($res['findings'])) {
                            continue;
                        }

                        // 2. FILTER: Only keep services used in this request
                        $serviceId = $res['service_id'] ?? null;
                        if ($serviceId && !isset($initializedServices[$serviceId])) {
                            continue;
                        }
                        
                        $rawCount += count($res['findings']);
                        
                        $file = $res['file_path'] ?? 'unknown';
                        $displayFile = $file;
                        if ($projectDir && str_starts_with($file, $projectDir)) {
                            $displayFile = ltrim(substr($file, strlen($projectDir)), '/');
                        }
                        
                        $auditResults[] = [
                            'file' => $displayFile,
                            'findings' => $res['findings']
                        ];
                    }
                } else {
                    $error = "No valid JSON found. Raw: " . $output;
                }
            } else {
                $error = "Igor failed (Exit $exitCode). " . $errorOutput;
            }
        }

        $this->data = [
            'audit_results' => $auditResults,
            'raw_count' => $rawCount,
            'binary_path' => (string) $this->igorBinaryPath,
            'binary_found' => (bool) $binaryFound,
            'config_path' => (string) $configPath,
            'config_found' => (bool) $configFound,
            'command' => (string) $command,
            'cwd' => (string) $cwd,
            'error' => (string) $error,
        ];
    }

    public function getRawCount(): int { return $this->data['raw_count'] ?? 0; }
    public function getAuditResults(): array { return $this->data['audit_results'] ?? []; }
    public function getBinaryPath(): string { return $this->data['binary_path'] ?? ''; }
    public function isBinaryFound(): bool { return $this->data['binary_found'] ?? false; }
    public function getConfigPath(): ?string { return $this->data['config_path'] ?? null; }
    public function isConfigFound(): bool { return $this->data['config_found'] ?? false; }
    public function getCommand(): string { return $this->data['command'] ?? ''; }
    public function getCwd(): string { return $this->data['cwd'] ?? ''; }
    public function getError(): ?string { return $this->data['error'] ?? null; }
    public function getName(): string { return 'igor'; }
    public function reset(): void { $this->data = []; }
}
