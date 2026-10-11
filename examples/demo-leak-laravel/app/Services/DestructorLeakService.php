<?php

namespace App\Services;

class DestructorLeakService
{
    private string $logPath;

    public function __construct()
    {
        // Use a persistent file on disk to trace life-cycle events across requests.
        // One log per runtime mode: both lab containers share the same storage/ directory.
        $mode = preg_replace('/[^a-z]/', '', strtolower((string) config('igor.runtime_mode'))) ?: 'unknown';
        $this->logPath = __DIR__ . '/../../storage/destructor_demo_' . $mode . '.log';

        $this->log("🟢 Constructor called");
    }

    public function __destruct()
    {
        $this->log("🔴 Destructor called");
    }

    public function getLogContent(): string
    {
        if (!file_exists($this->logPath)) {
            return "No events recorded yet.";
        }
        return file_get_contents($this->logPath);
    }

    public function clearLog(): void
    {
        if (file_exists($this->logPath)) {
            unlink($this->logPath);
        }
    }

    private function log(string $message): void
    {
        $time = date('H:i:s');
        $pid = getmypid();
        // Append life-cycle event to file
        file_put_contents(
            $this->logPath,
            "[$time] [PID: $pid] $message\n",
            FILE_APPEND | LOCK_EX
        );
    }
}
