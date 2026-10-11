<?php
namespace App;

use App\Cache\Registry;
use App\Cache\Store as Bag;
use App\Shared as Data;
use Symfony\Contracts\Service\ResetInterface;
use function Vendor\helper as Helper;

class ParentHolder { protected static array $cache = []; }

// DETECTED: static writes through a class name, parent:: or the current class name
class Writer extends ParentHolder
{
    private static int $own = 0;

    public function write(): void
    {
        Registry::$data[] = 'x';            // imported class
        Registry::$hits++;                  // increment
        \App\Cache\Registry::$data['k'] = 1; // fully-qualified
        Bag::$items[] = 1;                  // aliased import
        Data\Registry::$items[] = 1;        // qualified name through an aliased namespace
        parent::$cache[] = 'y';             // inherited static
        Writer::$own++;                     // current class by name
        writer::$own++;                     // class names are case-insensitive
        self::$own++;                       // control: already detected before
    }

    public function read(): array
    {
        $count = Registry::$hits;           // read only: not a mutation
        return [Registry::$data, $count];
    }

    public function ignored(): void
    {
        // @igor-ignore
        Registry::$data[] = 'z';
    }
}

// IGNORED: clearing another class's static inside reset() is a reset, not a leak
class RegistryCleaner
{
    public function reset(): void
    {
        Registry::$data = [];
    }
}

// IGNORED: a resettable class that resets the foreign static it fills
class ResettableRegistryUser implements ResetInterface
{
    public function add(string $v): void
    {
        Registry::$data[] = $v;
    }

    public function reset(): void
    {
        Registry::$data = [];
    }
}

// DETECTED (WARNING): the child redeclares $cache, so parent::$cache does not reset it
class ChildWithOwnCache extends ParentHolder implements ResetInterface
{
    protected static array $cache = [];

    public function add(string $v): void
    {
        self::$cache[] = $v;
    }

    public function reset(): void
    {
        parent::$cache = [];
    }
}

// DETECTED (WARNING): a resettable class fills another class's static but never resets it
class ForgetfulRegistryUser implements ResetInterface
{
    public function add(): void
    {
        Registry::$hits++;
    }

    public function reset(): void
    {
    }
}

// DETECTED: a dynamic scope is reported as written, not resolved against the namespace
class DynamicWriter
{
    public function write(string $class): void
    {
        $class::$data[] = 1;
    }
}

// IGNORED: @igor-ignore also applies to writes tracked for the reset check
class ResettableIgnoredWriter implements ResetInterface
{
    public function add(): void
    {
        // @igor-ignore
        Registry::$hits++;
    }

    public function reset(): void
    {
    }
}

// IGNORED: a cleanup on an excluded line in reset() still counts as a reset
class ResettableIgnoredCleanup implements ResetInterface
{
    public function add(): void
    {
        Registry::$hits++;
    }

    #[WorkerSafe]
    public function reset(): void
    {
        // @igor-ignore
        Registry::$hits = 0;
    }
}

// DETECTED: a dynamic scope is never matched against a reset, even in a resettable class
class ResettableDynamicWriter implements ResetInterface
{
    public function add(string $class): void
    {
        $class::$data[] = 1;
    }

    public function reset(): void
    {
        $class = Registry::class;
        $class::$data = [];
    }
}

// DETECTED: a function import is not a namespace alias
class FunctionImportWriter
{
    public function write(): void
    {
        Helper\Registry::$items[] = 1;
    }
}
