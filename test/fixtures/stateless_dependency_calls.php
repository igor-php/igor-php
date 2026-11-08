<?php

namespace App\Stateless;

// Shadows the global setlocale() for unqualified calls in this namespace
function setlocale(string $locale): string
{
    return $locale;
}

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

class TenantSwitcher
{
    public function setTenant(string $tenant): void
    {
        $_ENV['TENANT'] = $tenant;
    }
}

class PlaylistAdder
{
    public function addSongs(Playlist $playlist, User $user): void
    {
        $playlist->addSongs([1, 2], $user);
    }
}

class IndexHolder
{
    public function __construct(public Index $index)
    {
    }
}

class HeldIndexRenamer
{
    public function __construct(private IndexRenamer $renamer)
    {
    }

    public function setHeldName(IndexHolder $holder): void
    {
        $this->renamer->setRootName($holder->index);
    }
}

class LocalHelperRenamer
{
    public function setNameWith(Index $index): void
    {
        $helper = new IndexRenamer();
        $helper->setRootName($index);
    }
}

class IndexFactory
{
    public function createIndex(): Index
    {
        return new Index();
    }
}

class FactoryIndexRenamer
{
    public function __construct(private IndexRenamer $renamer)
    {
    }

    public function setFreshName(IndexFactory $factory): void
    {
        $this->renamer->setRootName($factory->createIndex());
    }
}

class IndexCollector
{
    public function __construct(private Index $own)
    {
    }

    public function setItems(Index $index): void
    {
        $items = collect([]);
        $items->push($index);
        $items->push($this->own);
    }
}

class NameSetter
{
    public function set(Index $index): void
    {
        $index->setName('renamed');
    }
}

class LocalSetHelper
{
    public function setWithHelper(Index $index): void
    {
        $helper = new NameSetter();
        $helper->set($index);
    }
}

class StoredIndexRenamer
{
    public function setFirstName(Index $index): void
    {
        $items = collect([]);
        $items->push($index);
        $items->first()->setName('renamed');
    }

    public function setAliasName(Index $index): void
    {
        $alias = $index;
        $alias->setName('renamed');
    }
}

class ReusedLocal
{
    public function __construct(private \ArrayObject $queue)
    {
    }

    public function setQueued(): void
    {
        $items = collect([]);
        $items = $this->queue;
        $items->append(1);
    }
}

class SelfToucher
{
    private int $touched = 0;

    public function setValue(): void
    {
        self::touch();
    }

    public function touch(): void
    {
        $this->touched++;
    }
}

class QuietToucher extends SelfToucher
{
    public function touch(): void
    {
    }
}

class FirstRenamer
{
    public function setFirst(array $items): void
    {
        $items[0]->setName('renamed');
    }
}

class WrappedRenamer
{
    public function __construct(private FirstRenamer $helper)
    {
    }

    public function setItems(Index $item): void
    {
        $this->helper->setFirst([$item]);
    }
}

class Counter
{
    public int $value = 0;
}

class CounterBumper
{
    public function setCount(Counter $counter): void
    {
        $counter->value++;
    }

    public function setAll(array $counters): void
    {
        foreach ($counters as $counter) {
            $counter->value = 1;
        }
    }
}

class Cursor
{
    private int $position = 0;

    public function advance(): void
    {
        $this->position++;
    }
}

class CursorUser
{
    public function __construct(private Cursor $cursor)
    {
    }

    public function setNext(): void
    {
        $cursor = $this->cursor;
        $cursor->advance();
    }
}

class QueueHolder
{
    public function __construct(private array $queues, private IndexHolder $holder)
    {
    }

    public function setCurrent(): void
    {
        $queue = current($this->queues);
        $queue->enqueue(1);
    }

    public function setCopyName(): void
    {
        $copy = clone $this->holder;
        $copy->index->setName('copy');
    }

    public function setCopy(): void
    {
        $copy = clone $this->holder;
        $copy->setIndexName('copy');
    }
}

class QuietCursor
{
    public function advance(): void
    {
    }
}

class BranchCursor
{
    public function __construct(private Cursor $mutable, private QuietCursor $quiet)
    {
    }

    public function setNext(bool $quiet): void
    {
        $cursor = $this->mutable;
        if ($quiet) {
            $cursor = $this->quiet;
        }
        $cursor->advance();
    }
}

class OptionsTrimmer
{
    public function setOptions(array $options): void
    {
        unset($options['temporary']);
        $options['count']++;
        $options['name'] = 'trimmed';
    }
}

class EncodingReader
{
    public function setEncodingCheck(): string
    {
        return mb_internal_encoding().setlocale('fr');
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
        private TenantSwitcher $tenantSwitcher,
        private HeldIndexRenamer $heldIndexRenamer,
        private IndexHolder $indexHolder,
        private LocalHelperRenamer $localHelperRenamer,
        private EncodingReader $encodingReader,
        private FactoryIndexRenamer $factoryRenamer,
        private IndexFactory $indexFactory,
        private IndexCollector $indexCollector,
        private LocalSetHelper $localSetHelper,
        private StoredIndexRenamer $storedRenamer,
        private ReusedLocal $reusedLocal,
        private QuietToucher $quietToucher,
        private WrappedRenamer $wrappedRenamer,
        private CounterBumper $bumper,
        private Counter $counter,
        private CursorUser $cursorUser,
        private QueueHolder $queueHolder,
        private BranchCursor $branchCursor,
        private OptionsTrimmer $optionsTrimmer,
        private array $options,
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

    public function switchTenant(): void
    {
        $this->tenantSwitcher->setTenant('acme');
    }

    public function renameHeldSharedIndex(): void
    {
        $this->heldIndexRenamer->setHeldName($this->indexHolder);
    }

    public function renameSharedIndexWithHelper(): void
    {
        $this->localHelperRenamer->setNameWith($this->index);
    }

    public function renameFreshIndexWithHelper(): void
    {
        $this->localHelperRenamer->setNameWith(new Index());
    }

    public function readEncoding(): void
    {
        $this->encodingReader->setEncodingCheck();
    }

    public function renameFromSharedFactory(): void
    {
        $this->factoryRenamer->setFreshName($this->indexFactory);
    }

    public function collectSharedIndex(): void
    {
        $this->indexCollector->setItems($this->index);
    }

    public function setSharedIndexWithHelper(): void
    {
        $this->localSetHelper->setWithHelper($this->index);
    }

    public function renameStoredSharedIndex(): void
    {
        $this->storedRenamer->setFirstName($this->index);
    }

    public function renameAliasedSharedIndex(): void
    {
        $this->storedRenamer->setAliasName($this->index);
    }

    public function pushThroughReusedLocal(): void
    {
        $this->reusedLocal->setQueued();
    }

    public function touchInherited(): void
    {
        $this->quietToucher->setValue();
    }

    public function renameWrappedSharedIndex(): void
    {
        $this->wrappedRenamer->setItems($this->index);
    }

    public function bumpSharedCounter(): void
    {
        $this->bumper->setCount($this->counter);
    }

    public function resetSharedCounters(): void
    {
        $this->bumper->setAll([$this->counter]);
    }

    public function advanceCursor(): void
    {
        $this->cursorUser->setNext();
    }

    public function pushCurrentQueue(): void
    {
        $this->queueHolder->setCurrent();
    }

    public function renameClonedChild(): void
    {
        $this->queueHolder->setCopyName();
    }

    public function renameClone(): void
    {
        $this->queueHolder->setCopy();
    }

    public function advanceBranchCursor(): void
    {
        $this->branchCursor->setNext(true);
    }

    public function trimSharedOptions(): void
    {
        $this->optionsTrimmer->setOptions($this->options);
    }
}
