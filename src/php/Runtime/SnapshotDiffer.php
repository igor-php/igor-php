<?php

namespace IgorPhp\IgorBundle\Runtime;

/**
 * Structural diff of two snapshot trees. Object identity is ignored on purpose:
 * a property reset to a fresh but equal object is not a leak.
 */
class SnapshotDiffer
{
    public const GROWTH = 'growth';
    public const OVERWRITE = 'overwrite';
    public const INITIALIZED = 'initialized';

    private const MAX_ADDED_SHOWN = 3;

    /**
     * @return list<array{path: string, kind: string, before: string, after: string, delta: int, added: string[]}>
     */
    public function diff(mixed $before, mixed $after): array
    {
        $changes = [];
        $this->compare('', $before, $after, $changes);

        return $changes;
    }

    private function compare(string $path, mixed $a, mixed $b, array &$changes): void
    {
        if ($a === $b) {
            return;
        }

        $typeA = $this->type($a);
        $typeB = $this->type($b);

        if ($typeA !== $typeB) {
            $kind = $typeA === 'lazy' && $typeB === 'object' ? self::INITIALIZED : self::OVERWRITE;
            $changes[] = $this->change($path, $kind, $a, $b);

            return;
        }

        switch ($typeA) {
            case 'array':
                $this->compareArrays($path, $a, $b, $changes);

                return;
            case 'object':
                if ($a['class'] !== $b['class']) {
                    $changes[] = $this->change($path, self::OVERWRITE, $a, $b);

                    return;
                }
                foreach ($a['props'] + $b['props'] as $name => $_) {
                    // Static state is keyed "::$prop" / "::method() static $var" by the snapshotter
                    $childPath = str_contains((string) $name, '::') ? $path . $name : $path . '->' . $name;
                    $this->compare($childPath, $a['props'][$name] ?? ['@t' => 'unset'], $b['props'][$name] ?? ['@t' => 'unset'], $changes);
                }

                return;
            case 'closure':
                if ($a['at'] !== $b['at']) {
                    $changes[] = $this->change($path, self::OVERWRITE, $a, $b);

                    return;
                }
                $this->compare($path . '{use}', $a['uses'], $b['uses'], $changes);

                return;
            default:
                $changes[] = $this->change($path, self::OVERWRITE, $a, $b);
        }
    }

    private function compareArrays(string $path, array $a, array $b, array &$changes): void
    {
        if ($b['n'] > $a['n']) {
            $added = [];
            foreach (array_diff_key($b['items'], $a['items']) as $key => $item) {
                if (count($added) === self::MAX_ADDED_SHOWN) {
                    break;
                }
                $added[] = '[' . $key . '] ' . $this->summarize($item);
            }
            $changes[] = $this->change($path, self::GROWTH, $a, $b, $b['n'] - $a['n'], $added);

            return;
        }

        if ($b['n'] < $a['n'] || array_keys($a['items']) !== array_keys($b['items'])) {
            $changes[] = $this->change($path, self::OVERWRITE, $a, $b, $b['n'] - $a['n']);

            return;
        }

        foreach ($a['items'] as $key => $item) {
            $this->compare($path . '[' . $key . ']', $item, $b['items'][$key], $changes);
        }
    }

    private function change(string $path, string $kind, mixed $a, mixed $b, int $delta = 0, array $added = []): array
    {
        return [
            'path' => $path,
            'kind' => $kind,
            'before' => $this->summarize($a),
            'after' => $this->summarize($b),
            'delta' => $delta,
            'added' => $added,
        ];
    }

    private function type(mixed $node): string
    {
        return is_array($node) ? $node['@t'] : 'scalar';
    }

    public function summarize(mixed $node): string
    {
        if (!is_array($node)) {
            $text = var_export($node, true);

            return strlen($text) > 60 ? substr($text, 0, 57) . '...' : $text;
        }

        switch ($node['@t']) {
            case 'array':
                return 'array(' . $node['n'] . ')';
            case 'object':
                return $node['class'];
            case 'lazy':
                return $node['class'] . ' (lazy, not loaded)';
            case 'closure':
                $uses = array_map(static fn ($name) => '$' . $name, array_keys($node['uses']['items'] ?? []));

                return 'Closure@' . $node['at'] . ($uses === [] ? '' : ' use (' . implode(', ', $uses) . ')');
            case 'service':
                return 'service(' . $node['id'] . ')';
            case 'ref':
                return '@ref ' . $node['to'];
            case 'opaque':
                return $node['desc'];
            case 'unset':
                return '(unset)';
            default:
                return '(' . $node['@t'] . ')';
        }
    }
}
