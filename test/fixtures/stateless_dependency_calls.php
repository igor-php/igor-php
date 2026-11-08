<?php

namespace App\Stateless;

interface RatingServiceInterface
{
    public function setRating(int $songId, int $rating): void;
}

// Writes to the database only: calling setRating() changes nothing in the worker
class RatingService implements RatingServiceInterface
{
    public function __construct(private RatingRepository $ratings)
    {
    }

    public function setRating(int $songId, int $rating): void
    {
        $this->assertRange($rating);
        $statistics = collect([$this->ratings->countFor($songId)]);
        $statistics->push($rating);
        Rating::query()->upsert(['song_id' => $songId, 'rating' => $rating], ['song_id'], ['rating']);
        $this->ratings->saveRating($songId, $rating);
    }

    private function assertRange(int $rating): void
    {
        if ($rating < 0 || $rating > 5) {
            throw new \InvalidArgumentException('Out of range');
        }
    }
}

class RatingRepository
{
    public function countFor(int $songId): int
    {
        return Rating::query()->where('song_id', $songId)->count();
    }

    public function saveRating(int $songId, int $rating): void
    {
        Rating::query()->insert(['song_id' => $songId, 'rating' => $rating]);
    }
}

// Real runtime state
class LocaleSwitcher
{
    private string $locale = 'en';

    public function setLocale(string $locale): void
    {
        $this->locale = $locale;
    }

    public function addLocales(array $locales): void
    {
        $this->storeLocale($locales[0]);
    }

    private function storeLocale(string $locale): void
    {
        $this->locale = $locale;
    }
}

class CompositeContext
{
    private \SplPriorityQueue $contexts;

    public function __construct()
    {
        $this->contexts = new \SplPriorityQueue();
    }

    public function addContext(object $context, int $priority = 0): void
    {
        $this->contexts->insert($context, $priority);
    }
}

class StaticRegistry
{
    private static array $handlers = [];

    public function registerHandler(string $name): void
    {
        static::$handlers[] = $name;
    }
}

abstract class BaseNotifier
{
    protected array $sent = [];

    public function setRecipient(string $recipient): void
    {
        $this->sent[] = $recipient;
    }
}

class MailNotifier extends BaseNotifier
{
    public function setRecipient(string $recipient): void
    {
        parent::setRecipient($recipient);
    }
}

trait CollectsMessages
{
    private array $messages = [];

    public function addMessage(string $message): void
    {
        $this->messages[] = $message;
    }
}

class FlashCollector
{
    use CollectsMessages;
}

class IndexRenamer
{
    public function setRootName(Index $index): void
    {
        $index->setName($index->getName().'_'.date('YmdHis'));
    }
}

class Index
{
    private string $name = 'songs';

    public function getName(): string
    {
        return $this->name;
    }

    public function setName(string $name): void
    {
        $this->name = $name;
    }
}

class AliasWriter
{
    private mixed $value = null;

    public function setValue(mixed $value): void
    {
        $self = $this;
        $other = $self;
        $other->value = $value;
    }
}

class RootRenamer
{
    public function __construct(private IndexRenamer $renamer, private Index $index)
    {
    }

    public function setRoot(): void
    {
        $this->renamer->setRootName($this->index);
    }
}

class TimezoneSetter
{
    public function setTimezone(string $timezone): void
    {
        date_default_timezone_set($timezone);
    }
}

class DynamicOptions
{
    public function __construct(private Options $options)
    {
    }

    public function setOption(string $method): void
    {
        $this->options->{$method}();
    }
}

class BaseValue
{
    public function setValue(mixed $value): void
    {
    }
}

class StoredValue extends BaseValue
{
    private mixed $value = null;

    public function setValue(mixed $value): void
    {
        $this->value = $value;
    }
}

class CacheWrapper
{
    public function clearCache(): void
    {
        Registry::flush();
    }
}

class PlaylistAdder
{
    public function addSongs(Playlist $playlist, User $user): void
    {
        $playlist->addSongs([1, 2], $user);
    }
}

final class StatelessCallsController
{
    public function __construct(
        private RatingService $ratingService,
        private RatingServiceInterface $ratingContract,
        private LocaleSwitcher $localeSwitcher,
        private CompositeContext $compositeContext,
        private StaticRegistry $registry,
        private BaseNotifier $baseNotifier,
        private MailNotifier $mailNotifier,
        private FlashCollector $flashCollector,
        private IndexRenamer $renamer,
        private Index $index,
        private UnknownService $unknown,
        private AliasWriter $aliasWriter,
        private RootRenamer $rootRenamer,
        private TimezoneSetter $timezoneSetter,
        private DynamicOptions $dynamicOptions,
        private BaseValue $baseValue,
        private CacheWrapper $cacheWrapper,
        private PlaylistAdder $playlistAdder,
        private Playlist $playlist,
        private User $user,
    ) {
    }

    public function rate(): void
    {
        $this->ratingService->setRating(1, 5);
    }

    public function rateThroughInterface(): void
    {
        $this->ratingContract->setRating(1, 5);
    }

    public function switchLocale(): void
    {
        $this->localeSwitcher->setLocale('fr');
    }

    public function addLocales(): void
    {
        $this->localeSwitcher->addLocales(['fr']);
    }

    public function addContext(): void
    {
        $this->compositeContext->addContext(new \stdClass());
    }

    public function register(): void
    {
        $this->registry->registerHandler('mail');
    }

    public function notifyAbstract(): void
    {
        $this->baseNotifier->setRecipient('a@example.com');
    }

    public function notifyParent(): void
    {
        $this->mailNotifier->setRecipient('a@example.com');
    }

    public function flash(): void
    {
        $this->flashCollector->addMessage('saved');
    }

    public function renameSharedIndex(): void
    {
        $this->renamer->setRootName($this->index);
    }

    public function renameFreshIndex(): void
    {
        $this->renamer->setRootName(new Index());
    }

    public function callUnknown(): void
    {
        $this->unknown->setValue(1);
    }

    public function writeThroughAlias(): void
    {
        $this->aliasWriter->setValue(1);
    }

    public function renameNestedSharedIndex(): void
    {
        $this->rootRenamer->setRoot();
    }

    public function changeTimezone(): void
    {
        $this->timezoneSetter->setTimezone('UTC');
    }

    public function setDynamicOption(): void
    {
        $this->dynamicOptions->setOption('enableDebug');
    }

    public function setOverriddenValue(): void
    {
        $this->baseValue->setValue(1);
    }

    public function clearStaticCache(): void
    {
        $this->cacheWrapper->clearCache();
    }

    public function addSongsForSharedUser(Playlist $playlist): void
    {
        $this->playlistAdder->addSongs($playlist, $this->user);
    }

    public function addSongsToSharedPlaylist(): void
    {
        $this->playlistAdder->addSongs($this->playlist, $this->user);
    }
}
