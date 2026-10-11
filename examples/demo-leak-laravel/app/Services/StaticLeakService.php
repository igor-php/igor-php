<?php
namespace App\Services;
class StaticLeakService {
    private static array $hist = [];
    public function touch(string $w): void { self::$hist[] = $w; }
    public function get(): array { return self::$hist; }
}
