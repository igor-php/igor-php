<?php

namespace App\Fresh;

final class Image
{
    // Each call returns a new closure with its own static variable
    public static function resolveOnce(\Closure $loader): \Closure
    {
        return static function () use ($loader) {
            static $contents; // not reported

            return $contents ??= $loader();
        };
    }

    public function counter(): int
    {
        static $calls = 0; // reported

        return ++$calls;
    }

    public function storeResolver(): void
    {
        // The closure lives as long as the service: its static variable too
        $this->resolver = function () {
            static $cache = []; // reported

            return $cache;
        };
    }
}

final class LibraryStatisticsService
{
    public function __construct(private SummaryService $summary, private Collection $items, private Registry $registry)
    {
    }

    public function getStatistics(): Collection
    {
        $statistics = collect([$this->summary->getSongCount()]);
        $statistics->push($this->summary->getAlbumCount()); // not reported

        $copy = clone $this->items;
        $copy->push(1); // not reported

        $items = $this->items;
        $items->push(1); // reported

        $fallback = $this->items ?? new Collection();
        $fallback->push(1); // reported

        $kernel = self::getKernel()->getContainer();
        $kernel->set('id', new \stdClass()); // reported

        $refs = [$this->items];
        $queue = $refs[0];
        $queue->push(1); // reported

        $handler = HandlerProvider::forRegistry($this->registry);
        $handler->setDefault('x'); // reported

        $current = current($this->queues);
        $current->push(1); // reported

        $snapshot = clone $this->holder;
        $snapshot->queue->push(1); // reported
        $snapshot->setName('copy'); // not reported

        return $statistics;
    }
}
