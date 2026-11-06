<?php

namespace App\Service;

class UnsafeRestores
{
    private bool $running = false;
    private bool $appendNewline = true;
    private bool $handleMissing = true;
    private ?object $current = null;
    private bool $enabled = false;

    // Reported once: restored in catch (\Exception), a \Error skips it
    public function dispatch(object $message): object
    {
        $this->running = true;
        try {
            $result = $this->handle($message);
        } catch (\Exception $e) {
            $this->running = false; // reported
            throw $e;
        }
        $this->running = false;

        return $result;
    }

    // Reported: saved and restored without finally
    public function formatBatch(array $records): string
    {
        $oldNewline = $this->appendNewline;
        $this->appendNewline = false;
        $out = implode("\n", array_map([$this, 'format'], $records));
        $this->appendNewline = $oldNewline; // reported

        return $out;
    }

    // Reported: switched off around a user callback, switched back on without finally
    public function missingKey(string $key, callable $callback): string
    {
        $this->handleMissing = false;
        $key = $callback($key);
        $this->handleMissing = true; // reported

        return $key;
    }

    // Reported: reset to null after the work, without finally
    public function withCurrent(object $request): void
    {
        $this->current = $request;
        $this->handle($request);
        $this->current = null; // reported
    }

    // Not reported: restored in finally
    public function safeFinally(object $message): object
    {
        $previous = $this->running;
        $this->running = true;
        try {
            return $this->handle($message);
        } finally {
            $this->running = $previous; // not reported
        }
    }

    // Not reported: restored in catch (\Throwable) and after the try
    public function safeThrowable(object $message): object
    {
        $this->running = true;
        try {
            $result = $this->handle($message);
        } catch (\Throwable $e) {
            $this->running = false; // not reported
            throw $e;
        }
        $this->running = false; // not reported

        return $result;
    }

    // Not reported: catch (\Exception | \Error) covers everything
    public function safeUnion(object $message): object
    {
        $this->running = true;
        try {
            $result = $this->handle($message);
        } catch (\Exception|\Error $e) {
            $this->running = false; // not reported
            throw $e;
        }
        $this->running = false; // not reported

        return $result;
    }

    // Not reported: restored at the end of the try body and in catch (\Throwable)
    public function safeMutex(): void
    {
        $this->enabled = true;
        try {
            $this->handle(new \stdClass());
            $this->enabled = false; // not reported
        } catch (\Throwable $e) {
            $this->handle($e);
            $this->enabled = false; // not reported
        }
    }

    // Not reported: a flag set back in a condition is no restoration
    public function run(): void
    {
        $this->enabled = false;
        if ($this->handle(new \stdClass())) {
            $this->enabled = true; // not reported

            return;
        }
    }

    // Not reported: nothing can throw between the two writes
    public function toggle(): void
    {
        $this->enabled = true;
        $this->enabled = false; // not reported
    }

    // Not reported: two branches, not a set and a restore
    public function configure(bool $on): void
    {
        if ($on) {
            $this->enabled = true;
            $this->handle(new \stdClass());
        } else {
            $this->enabled = false; // not reported
        }
    }

    // Not reported: the closure runs later, in another scope
    public function deferred(): callable
    {
        $this->enabled = true;
        $this->handle(new \stdClass());

        return function () {
            $this->enabled = false; // not reported
        };
    }

    private function handle(object $message): object
    {
        return $message;
    }

    private function format(array $record): string
    {
        return json_encode($record) . ($this->appendNewline ? "\n" : '');
    }
}
