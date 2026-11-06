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
            $this->enabled = false; // not reported
            $this->handle($e);
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

    // Reported: unqualified in this namespace, Exception and Error name App\Service classes
    public function unqualifiedCatch(object $message): object
    {
        $this->running = true;
        try {
            $result = $this->handle($message);
        } catch (Exception|Error $e) {
            $this->running = false; // reported
            throw $e;
        }
        $this->running = false;

        return $result;
    }

    // Reported: the finally only restores the first write, not the second one
    public function twoPhases(object $message): void
    {
        $this->enabled = true;
        try {
            $this->handle($message);
        } finally {
            $this->enabled = false; // not reported
        }
        $this->enabled = true;
        $this->handle($message);
        $this->enabled = false; // reported
    }

    // Reported: set in the try body, restored after the try statement
    public function afterTry(object $message): void
    {
        try {
            $this->enabled = true;
            $this->handle($message);
        } catch (\RuntimeException $e) {
            $this->handle($e);
        }
        $this->enabled = false; // reported
    }

    // Not reported: the variable no longer holds the saved property
    public function overwrittenSave(object $message): void
    {
        $value = $this->current;
        $value = $this->handle($message);
        $this->current = $message;
        $this->handle($message);
        $this->current = $value; // not reported
    }

    // Reported: the catch overwrites the saved value, but it rethrows, so the restore still uses it
    public function overwrittenInCatch(object $request): void
    {
        $old = $this->current;
        $this->current = $request;
        try {
            $this->handle($request);
        } catch (\Exception $e) {
            $old = null;
            throw $e;
        }
        $this->current = $old; // reported
    }

    // Not reported: catch (\Throwable) swallows every exception, the restore always runs
    public function swallowed(object $message): void
    {
        try {
            $this->enabled = true;
            $this->handle($message);
        } catch (\Throwable $e) {
        }
        $this->enabled = false; // not reported
    }

    // Reported: catch (\Throwable) rethrows, the restore after the try is skipped
    public function rethrown(object $message): void
    {
        try {
            $this->enabled = true;
            $this->handle($message);
        } catch (\Throwable $e) {
            throw $e;
        }
        $this->enabled = false; // reported
    }

    // Not reported: both branches overwrite the saved value and go on, the restore is no restoration
    public function overwrittenInBranches(object $request, bool $flag): void
    {
        $old = $this->current;
        $this->current = $request;
        if ($flag) {
            $old = $request;
        } else {
            $old = null;
        }
        $this->handle($request);
        $this->current = $old; // not reported
    }

    // Reported: catch (\Throwable) swallows, but the earlier catch returns before the restore
    public function returningCatch(object $message): void
    {
        try {
            $this->enabled = true;
            $this->handle($message);
        } catch (\RuntimeException $e) {
            return;
        } catch (\Throwable $e) {
        }
        $this->enabled = false; // reported
    }

    // Reported: when $flag is false, $old still holds the saved value
    public function conditionalOverwrite(object $request, bool $flag): void
    {
        $old = $this->current;
        $this->current = $request;
        if ($flag) {
            $old = $request;
        }
        $this->handle($request);
        $this->current = $old; // reported
    }

    // Not reported: the break only ends the loop of the catch, the catch-all swallows the rest
    public function loopInCatch(object $message, array $items): void
    {
        try {
            $this->enabled = true;
            $this->handle($message);
        } catch (\RuntimeException $e) {
            foreach ($items as $item) {
                if ($item) {
                    break;
                }
            }
        } catch (\Throwable $e) {
        }
        $this->enabled = false; // not reported
    }

    // Reported: the else branch only returns on $stop, otherwise $old still holds the saved value
    public function conditionalReturn(object $request, bool $flag, bool $stop): void
    {
        $old = $this->current;
        $this->current = $request;
        if ($flag) {
            $old = null;

            return;
        } else {
            if ($stop) {
                return;
            }
        }
        $this->handle($request);
        $this->current = $old; // reported
    }

    // Reported: break 2 leaves the outer loop, past the restore
    public function multiLevelBreak(array $messages, array $items): void
    {
        foreach ($messages as $message) {
            try {
                $this->enabled = true;
                $this->handle($message);
            } catch (\RuntimeException $e) {
                foreach ($items as $item) {
                    break 2;
                }
            } catch (\Throwable $e) {
            }
            $this->enabled = false; // reported
        }
    }

    // Reported: the call before the cleanup in finally can throw and skip it
    public function callBeforeCleanup(object $message): void
    {
        $this->enabled = true;
        try {
            $this->handle($message);
        } finally {
            $this->handle($message);
            $this->enabled = false; // reported
        }
    }

    // Not reported: the call before the cleanup in finally is guarded by its own catch (\Throwable)
    public function guardedCleanup(object $message): void
    {
        $this->enabled = true;
        try {
            $this->handle($message);
        } finally {
            try {
                $this->handle($message);
            } catch (\Throwable $e) {
            }
            $this->enabled = false; // not reported
        }
    }

    // Not reported: set before the try, every exception of the try is swallowed before the restore
    public function swallowedAfterWrite(object $message): void
    {
        $this->enabled = true;
        try {
            $this->handle($message);
        } catch (\Throwable $e) {
        }
        $this->enabled = false; // not reported
    }

    // Reported: the call after the swallowing try can still throw past the restore
    public function throwsAfterSwallow(object $message): void
    {
        $this->enabled = true;
        try {
            $this->handle($message);
        } catch (\Throwable $e) {
        }
        $this->handle($message);
        $this->enabled = false; // reported
    }

    private array $stack = [];

    // Reported: array_pop() on a local value throws a TypeError when it is null
    public function popLocal(?array $items): void
    {
        $this->enabled = true;
        array_pop($items);
        $this->enabled = false; // reported
    }

    // Not reported: array_pop() on the service's own stack does not throw
    public function popOwnStack(object $message): void
    {
        $this->enabled = true;
        try {
            $this->handle($message);
        } finally {
            array_pop($this->stack);
            $this->enabled = false; // not reported
        }
    }

    // Not reported: the named form array_pop(array: ...) is the same cleanup
    public function popNamed(object $message): void
    {
        $this->enabled = true;
        try {
            $this->handle($message);
        } finally {
            array_pop(array: $this->stack);
            $this->enabled = false; // not reported
        }
    }

    // Not reported: the value is saved after the write, the last assignment restores nothing
    public function lateSave(object $request): void
    {
        $this->current = $request;
        $old = $this->current;
        $this->handle($request);
        $this->current = $old; // not reported
    }

    // Reported: the save after the write only runs on some paths, the first one still reaches
    public function conditionalLateSave(object $request, bool $flag): void
    {
        $old = $this->current;
        $this->current = $request;
        if ($flag) {
            $old = $this->current;
        }
        $this->handle($request);
        $this->current = $old; // reported
    }

    // Not reported: both branches save the temporary value, the last assignment restores nothing
    public function lateSaveInBranches(object $request, bool $flag): void
    {
        $old = $this->current;
        $this->current = $request;
        if ($flag) {
            $old = $this->current;
        } else {
            $old = $this->current;
        }
        $this->handle($request);
        $this->current = $old; // not reported
    }

    // Reported: the finally restores the temporary value when the condition saved it again
    public function conditionalSaveInFinally(object $request, bool $flag): void
    {
        $old = $this->current;
        $this->current = $request;
        try {
            $this->handle($request);
            $this->current = $old; // reported
        } finally {
            if ($flag) {
                $old = $this->current;
            }
            $this->current = $old;
        }
    }

    // Not reported: the overwrite returns before the write, every path to finally keeps the save
    public function earlyReturnOverwrite(object $request, bool $skip): void
    {
        $old = $this->current;
        if ($skip) {
            $old = null;
            return;
        }
        $this->current = $request;
        try {
            $this->handle($request);
        } finally {
            $this->current = $old; // not reported
        }
    }

    // Reported: the throw still runs the finally, which restores the value saved again
    public function throwingOverwrite(object $request, bool $fail): void
    {
        $old = $this->current;
        $this->current = $request;
        try {
            if ($fail) {
                $old = $this->current;
                throw new \RuntimeException();
            }
            $this->handle($request);
            $this->current = $old; // reported
        } finally {
            $this->current = $old;
        }
    }

    // Not reported: the return leaves through another try statement, before the write
    public function earlyReturnInOtherTry(object $request, bool $skip): void
    {
        $old = $this->current;
        try {
            if ($skip) {
                $old = null;
                return;
            }
        } finally {
        }
        $this->current = $request;
        try {
            $this->handle($request);
        } finally {
            $this->current = $old; // not reported
        }
    }

    // Reported: the throw is caught, the save of the temporary value reaches the finally
    public function caughtThrowOverwrite(object $request, bool $flag): void
    {
        $old = $this->current;
        $this->current = $request;
        try {
            if ($flag) {
                $old = $this->current;
                throw new \RuntimeException();
            }
        } catch (\Throwable $e) {
        }
        try {
            $this->handle($request);
            $this->current = $old; // reported
        } finally {
            $this->current = $old;
        }
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
