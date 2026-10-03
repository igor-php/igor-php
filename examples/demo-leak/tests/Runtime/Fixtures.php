<?php

namespace App\Tests\Runtime;

use IgorPhp\IgorBundle\Attribute\WorkerSafe;

class Address
{
    public string $city = 'Paris';
}

class Owner
{
    public Address $address;
    public ?Cart $cart = null;

    public function __construct()
    {
        $this->address = new Address();
    }
}

class Cart
{
    public array $items = [];
    public Owner $owner;
    public ?\Closure $callback = null;
    public ?object $collaborator = null;
    private static array $history = [];

    #[WorkerSafe(reason: 'request-scoped scratch value')]
    public array $scratch = [];

    public function __construct()
    {
        $this->owner = new Owner();
    }

    public function remember(string $entry): void
    {
        self::$history[] = $entry;
    }

    public function nextNumber(): int
    {
        static $number = 0;

        return ++$number;
    }

    public static function forget(): void
    {
        self::$history = [];
    }
}

class Heavy
{
    public string $payload;

    public function __construct()
    {
        $this->payload = 'loaded';
    }
}
