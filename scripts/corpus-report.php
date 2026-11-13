<?php

/*
 * Regression corpus report (#152), called by scripts/corpus.sh for each project.
 *
 * It checks the invariants (blocking), counts the findings per rule and severity, and compares the
 * counts with the project's baseline (a warning, not a failure). The report is printed as Markdown,
 * and appended to $GITHUB_STEP_SUMMARY in CI.
 *
 * Exit code: 1 when an invariant fails, 0 otherwise.
 */

$options = getopt('', ['name:', 'framework:', 'commit:', 'dir:', 'results:', 'log:', 'igor-status:', 'baseline:', 'update']);
foreach (['name', 'framework', 'commit', 'dir', 'results', 'log', 'igor-status', 'baseline'] as $required) {
    if (!isset($options[$required])) {
        fwrite(STDERR, "missing --$required\n");
        exit(2);
    }
}

$name = $options['name'];
$framework = $options['framework'];
$dir = rtrim($options['dir'], '/');
$failures = [];

// 1. igor ran the Deep Audit and produced a report
$log = (string) @file_get_contents($options['log']);
if ((int) $options['igor-status'] !== 0) {
    $failures[] = sprintf('igor exited with status %d (see %s)', $options['igor-status'], $options['log']);
}
if (preg_match('/Falling back to standard directory scan|Deep Audit disabled/', $log)) {
    $failures[] = 'igor fell back to a directory scan instead of the Deep Audit';
}
$results = json_decode((string) @file_get_contents($options['results']), true);
if (!is_array($results)) {
    $failures[] = 'igor printed no JSON report';
    $results = [];
}

// 2. Framework invariants
$entityFindings = [];
if ($framework === 'symfony') {
    $map = json_decode((string) @file_get_contents("$dir/var/cache/dev/igor_service_map.json"), true);
    if (!is_array($map)) {
        $failures[] = 'no service map in var/cache/dev/igor_service_map.json';
    } else {
        $errored = array_filter(array_keys($map['definitions'] ?? []), static fn (string $id): bool => str_starts_with($id, '.errored.'));
        if ($errored !== []) {
            $failures[] = sprintf('%d .errored. placeholders in the service map, e.g. %s', count($errored), reset($errored));
        }
    }

    // A Doctrine entity is built per row: it is never a shared service (#138)
    $isEntity = [];
    foreach ($results as $status) {
        $file = $status['file_path'] ?? '';
        if (!isset($isEntity[$file])) {
            $source = (string) @file_get_contents($file);
            $isEntity[$file] = (bool) preg_match('/#\[\s*(?:ORM\\\\)?Entity\b|@ORM\\\\Entity\b/', $source);
        }
        if ($isEntity[$file] && ($status['findings'] ?? []) !== []) {
            $entityFindings[] = sprintf('%s (%d findings)', relative($file, $dir), count($status['findings']));
        }
    }
    if ($entityFindings !== []) {
        $failures[] = sprintf('findings in %d Doctrine entities: %s', count($entityFindings), implode(', ', array_slice($entityFindings, 0, 5)));
    }
}

// 3. Counts per rule and severity, compared with the baseline
$counts = [];
$files = 0;
foreach ($results as $status) {
    if (($status['findings'] ?? []) !== []) {
        ++$files;
    }
    foreach ($status['findings'] ?? [] as $finding) {
        $key = ($finding['rule_id'] ?? '?').' '.($finding['severity'] ?? '?');
        $counts[$key] = ($counts[$key] ?? 0) + 1;
    }
}
ksort($counts);

$baseline = json_decode((string) @file_get_contents($options['baseline']), true);
$before = is_array($baseline) ? ($baseline['counts'] ?? []) : null;

$lines = ["### Corpus: $name", ''];
if ($failures !== []) {
    $lines[] = '**Invariants failed:**';
    foreach ($failures as $failure) {
        $lines[] = "- ❌ $failure";
    }
} else {
    $lines[] = '✅ Invariants hold.';
}
$lines[] = '';

$changed = false;
if ($before === null) {
    $lines[] = sprintf('No baseline yet (%s): run `make corpus-update`.', relative($options['baseline'], getcwd() ?: '.'));
    $lines[] = '';
    $lines[] = '| Rule | Findings |';
    $lines[] = '|---|---|';
    foreach ($counts as $key => $count) {
        $lines[] = "| $key | $count |";
    }
} else {
    $lines[] = '| Rule | Baseline | Now | Δ |';
    $lines[] = '|---|---|---|---|';
    $keys = array_unique(array_merge(array_keys($before), array_keys($counts)));
    sort($keys);
    foreach ($keys as $key) {
        $old = $before[$key] ?? 0;
        $new = $counts[$key] ?? 0;
        $delta = $new - $old;
        $changed = $changed || $delta !== 0;
        $lines[] = sprintf('| %s | %d | %d | %s |', $key, $old, $new, $delta === 0 ? '' : sprintf('%+d', $delta));
    }
    $baselineFiles = $baseline['files'] ?? 0;
    // Findings may move between files with the same totals per rule
    $changed = $changed || $files !== $baselineFiles;
    $lines[] = sprintf('| files with findings | %d | %d | %s |', $baselineFiles, $files, $files === $baselineFiles ? '' : sprintf('%+d', $files - $baselineFiles));
    $lines[] = '';
    $lines[] = $changed
        ? '⚠️ The findings changed: check the report in '.$options['results'].', then run `make corpus-update` if the change is intended.'
        : 'No change with the baseline.';
}

$report = implode("\n", $lines)."\n";
echo "\n", $report;
if ($summary = getenv('GITHUB_STEP_SUMMARY')) {
    file_put_contents($summary, $report."\n", FILE_APPEND);
}
if ($changed && getenv('GITHUB_ACTIONS')) {
    echo "::warning title=Corpus $name::The findings changed from the baseline\n";
}

if (isset($options['update'])) {
    if ($failures !== []) {
        fwrite(STDERR, "Baseline of $name not updated: an invariant fails.\n");
    } else {
        $data = ['project' => $name, 'commit' => $options['commit'], 'files' => $files, 'counts' => $counts];
        file_put_contents($options['baseline'], json_encode($data, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES)."\n");
        echo "Baseline written: {$options['baseline']}\n";
    }
}

exit($failures === [] ? 0 : 1);

function relative(string $path, string $base): string
{
    return str_starts_with($path, $base.'/') ? substr($path, strlen($base) + 1) : $path;
}
