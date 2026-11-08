<?php

/*
 * PostToolUse hook of the igor Claude Code plugin.
 *
 * After Claude edits a PHP file, it audits that file with igor-php and hands back to Claude the
 * findings on the lines it just wrote, so a state leak is fixed while the code is still fresh.
 *
 * It only informs: it never blocks the edit, since igor is deliberately strict. It stays silent
 * when the watch is off (no .claude/igor-watch file and no IGOR_WATCH=1), when the file is not
 * application PHP code, when igor-php is not installed, or when the edited lines have no finding.
 */

declare(strict_types=1);

const SKIPPED_DIRECTORIES = ['vendor', 'var', 'node_modules', 'storage', 'bootstrap/cache', 'tests', 'test', 'database', 'resources/views', 'migrations'];

exit(main());

function main(): int
{
    $input = json_decode((string) stream_get_contents(STDIN), true);
    if (!is_array($input)) {
        return 0;
    }

    $projectDir = (string) (getenv('CLAUDE_PROJECT_DIR') ?: ($input['cwd'] ?? getcwd()));
    if (!watchEnabled($projectDir)) {
        return 0;
    }

    $toolInput = is_array($input['tool_input'] ?? null) ? $input['tool_input'] : [];
    $file = (string) ($toolInput['file_path'] ?? '');
    if (!isAuditedFile($file, $projectDir)) {
        return 0;
    }

    $binary = findIgorBinary($projectDir);
    if ($binary === null) {
        return 0;
    }

    $results = auditFile($binary, $file, $projectDir);
    if ($results === null) {
        return 0;
    }

    $content = (string) @file_get_contents($file);
    $toolResponse = is_array($input['tool_response'] ?? null) ? $input['tool_response'] : [];
    $lines = patchedLines($toolResponse) ?? changedLines((string) ($input['tool_name'] ?? ''), $toolInput, $content);
    $findings = findingsOnLines($results, $lines);
    if ($findings === []) {
        return 0;
    }

    echo json_encode([
        'hookSpecificOutput' => [
            'hookEventName' => 'PostToolUse',
            'additionalContext' => formatContext($findings, relativePath($file, $projectDir)),
        ],
    ], JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE), "\n";

    return 0;
}

function watchEnabled(string $projectDir): bool
{
    $env = getenv('IGOR_WATCH');
    if ($env !== false && $env !== '') {
        return in_array(strtolower($env), ['1', 'true', 'on', 'yes'], true);
    }

    return is_file($projectDir.'/.claude/igor-watch');
}

function isAuditedFile(string $file, string $projectDir): bool
{
    if ($file === '' || !str_ends_with($file, '.php') || str_ends_with($file, '.blade.php') || !is_file($file)) {
        return false;
    }

    $relative = relativePath($file, $projectDir);
    foreach (SKIPPED_DIRECTORIES as $directory) {
        if (str_starts_with($relative, $directory.'/') || str_contains($relative, '/'.$directory.'/')) {
            return false;
        }
    }

    return true;
}

function findIgorBinary(string $projectDir): ?string
{
    $override = getenv('IGOR_BIN');
    if ($override !== false && $override !== '') {
        return is_file($override) ? $override : null;
    }

    $local = $projectDir.'/vendor/bin/igor-php';
    if (is_file($local)) {
        return $local;
    }

    foreach (['igor-php', 'igor'] as $name) {
        foreach (explode(PATH_SEPARATOR, (string) getenv('PATH')) as $directory) {
            $candidate = $directory.DIRECTORY_SEPARATOR.$name;
            if ($directory !== '' && is_file($candidate) && is_executable($candidate)) {
                return $candidate;
            }
        }
    }

    return null;
}

/**
 * Runs igor-php on the file. When the deep audit cannot start (Symfony cache not warmed up,
 * igor:dump failing...), it falls back to --no-agent: a less precise audit beats no audit.
 *
 * @return list<array<string, mixed>>|null
 */
function auditFile(string $binary, string $file, string $projectDir): ?array
{
    $command = str_ends_with($binary, '.php') || isPhpScript($binary) ? [PHP_BINARY, $binary] : [$binary];

    foreach ([[], ['--no-agent']] as $extraFlags) {
        // Flags must come before the target: igor stops reading flags at the first argument.
        $output = run([...$command, ...$extraFlags, '--output', 'json', $file], $projectDir);
        $decoded = json_decode($output, true);
        if (is_array($decoded)) {
            return array_values(array_filter($decoded, 'is_array'));
        }
    }

    return null;
}

function isPhpScript(string $binary): bool
{
    $handle = @fopen($binary, 'r');
    if ($handle === false) {
        return false;
    }
    $firstLine = (string) fgets($handle);
    fclose($handle);

    return str_starts_with($firstLine, '#!') && str_contains($firstLine, 'php');
}

/**
 * @param list<string> $command
 */
function run(array $command, string $cwd): string
{
    $process = proc_open($command, [0 => ['pipe', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes, $cwd);
    if (!is_resource($process)) {
        return '';
    }
    fclose($pipes[0]);
    $stdout = (string) stream_get_contents($pipes[1]);
    stream_get_contents($pipes[2]);
    fclose($pipes[1]);
    fclose($pipes[2]);
    proc_close($process);

    return $stdout;
}

/**
 * Returns the lines the edit added, read from the diff Claude Code reports with the tool result
 * (structuredPatch hunks: " " context, "-" removed, "+" added), or null when there is no diff.
 *
 * @param array<string, mixed> $toolResponse
 *
 * @return array<int, true>|null
 */
function patchedLines(array $toolResponse): ?array
{
    $hunks = $toolResponse['structuredPatch'] ?? null;
    if (!is_array($hunks) || $hunks === []) {
        return null;
    }

    $lines = [];
    foreach ($hunks as $hunk) {
        if (!is_array($hunk) || !isset($hunk['newStart']) || !is_array($hunk['lines'] ?? null)) {
            return null;
        }
        $line = (int) $hunk['newStart'];
        foreach ($hunk['lines'] as $diffLine) {
            $marker = substr((string) $diffLine, 0, 1);
            if ($marker === '+') {
                $lines[$line] = true;
            }
            if ($marker !== '-' && $marker !== '\\') {
                ++$line;
            }
        }
    }

    return $lines;
}

/**
 * Fallback without a diff: finds the edited text in the file. Text that occurs several times
 * selects all its occurrences.
 *
 * Returns the lines Claude just wrote, or null when every line counts (a whole file was written,
 * or the edited text cannot be found any more).
 *
 * @param array<string, mixed> $toolInput
 *
 * @return array<int, true>|null
 */
function changedLines(string $toolName, array $toolInput, string $content): ?array
{
    $newStrings = match ($toolName) {
        'Edit' => [(string) ($toolInput['new_string'] ?? '')],
        'MultiEdit' => array_map(
            static fn ($edit): string => is_array($edit) ? (string) ($edit['new_string'] ?? '') : '',
            is_array($toolInput['edits'] ?? null) ? $toolInput['edits'] : [],
        ),
        default => null,
    };
    if ($newStrings === null) {
        return null;
    }

    $lines = [];
    foreach ($newStrings as $newString) {
        if (trim($newString) === '') {
            continue;
        }
        $offset = 0;
        $found = false;
        while (($position = strpos($content, $newString, $offset)) !== false) {
            $found = true;
            $first = substr_count($content, "\n", 0, $position) + 1;
            $last = $first + substr_count($newString, "\n");
            for ($line = $first; $line <= $last; ++$line) {
                $lines[$line] = true;
            }
            $offset = $position + max(1, strlen($newString));
        }
        if (!$found) {
            return null;
        }
    }

    return $lines;
}

/**
 * @param list<array<string, mixed>> $results
 * @param array<int, true>|null      $lines
 *
 * @return list<array<string, mixed>>
 */
function findingsOnLines(array $results, ?array $lines): array
{
    $findings = [];
    foreach ($results as $result) {
        foreach (is_array($result['findings'] ?? null) ? $result['findings'] : [] as $finding) {
            if (!is_array($finding) || !empty($finding['ignored'])) {
                continue;
            }
            $line = (int) ($finding['line'] ?? 0);
            if ($lines === null || $line === 0 || isset($lines[$line])) {
                $findings[] = $finding;
            }
        }
    }

    usort($findings, static fn (array $a, array $b): int => ((int) ($a['line'] ?? 0)) <=> ((int) ($b['line'] ?? 0)));

    return $findings;
}

/**
 * @param list<array<string, mixed>> $findings
 */
function formatContext(array $findings, string $relativePath): string
{
    $text = sprintf(
        "igor-php (FrankenPHP worker mode audit) reports %d finding(s) on the lines you just changed in %s:\n",
        count($findings),
        $relativePath,
    );
    foreach ($findings as $finding) {
        $text .= sprintf(
            "- line %d [%s] %s Fix: %s\n",
            (int) ($finding['line'] ?? 0),
            (string) ($finding['severity'] ?? 'WARNING'),
            (string) ($finding['message'] ?? ''),
            (string) ($finding['remediation'] ?? ''),
        );
    }

    return $text."In worker mode this state survives from one request to the next. Fix it now following the frankenphp-worker-audit skill, or tell the user why it is safe (e.g. a bounded cache filled at boot) before going on.";
}

function relativePath(string $file, string $projectDir): string
{
    // Windows paths use backslashes: compare and report with forward slashes.
    $file = str_replace('\\', '/', $file);
    $projectDir = rtrim(str_replace('\\', '/', $projectDir), '/');
    if ($projectDir !== '' && str_starts_with($file, $projectDir.'/')) {
        return substr($file, strlen($projectDir) + 1);
    }

    return $file;
}
