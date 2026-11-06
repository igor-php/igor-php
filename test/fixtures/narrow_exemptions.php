<?php

namespace App\Service;

use Symfony\Component\HttpFoundation\Request;

// #105: writes that cannot carry state from one request to the next are not reported,
// while the look-alike patterns that can still are.
final class NarrowExemptions
{
    private $lexer;
    private $parser;
    private $locale;
    private $formatter;
    private $tokenStorage;
    private $instances = [];
    private $connection;
    private $cache = [];
    private static $registry = [];

    public function __construct(private $dependency) {}

    public function lexer()
    {
        return $this->lexer ??= new Lexer();                          // not reported: same object for every request
    }

    public function parser()
    {
        return $this->parser ??= new Parser($this->dependency, 10);   // not reported: built from an injected service
    }

    public function locale(Request $request)
    {
        return $this->locale ??= $request->getLocale();               // reported: the first request wins
    }

    public function formatter($format)
    {
        return $this->formatter ??= new Formatter($format);           // reported: built from a parameter
    }

    public function tokenStorage()
    {
        return $this->tokenStorage ??= new Holder($this->tokenSource); // reported: may capture a token
    }

    public function clear(): void
    {
        $this->instances = [];                                        // not reported: this write is the reset
        self::$registry = [];                                         // not reported
    }

    public function close(): void
    {
        $this->connection = null;                                     // not reported
    }

    public function clearCache($key): void
    {
        $this->cache = [];                                            // reported: a prefix is not a reset method
    }

    public function closeWith($connection): void
    {
        $this->connection = $connection;                              // reported: not an empty value
    }

    public function env(): string
    {
        $_ENV['APP_THEME'] = 'dark';                                  // reported: a write poisons the process
        unset($_ENV['APP_DEBUG']);                                    // reported

        return $_ENV['APP_ENV'] ?? 'prod';                            // not reported: process environment
    }

    public function server(): string
    {
        return $_SERVER['REQUEST_URI'];                               // reported: request data
    }
}
