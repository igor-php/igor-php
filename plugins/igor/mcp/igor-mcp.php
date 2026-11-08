<?php

/**
 * Starts the igor MCP server (`igor-php mcp`) for the project Claude Code works on.
 *
 * It uses IGOR_BIN, then vendor/bin/igor-php, then igor-php or igor on the PATH. stdin, stdout and stderr
 * are handed to the server as they are: stdout only carries the MCP protocol.
 */

declare(strict_types=1);

$projectDir = (string) (getenv('CLAUDE_PROJECT_DIR') ?: getcwd());
$binary = findIgorBinary($projectDir);
if ($binary === null) {
    fwrite(STDERR, "igor-php is not installed in {$projectDir}: run `composer require --dev igor-php/igor-php`, or set IGOR_BIN.\n");
    exit(1);
}

$command = isPhpScript($binary) ? [PHP_BINARY, $binary] : [$binary];
$command = array_merge($command, ['mcp'], array_slice($argv, 1), [$projectDir]);

$process = proc_open($command, [0 => STDIN, 1 => STDOUT, 2 => STDERR], $pipes, $projectDir);
if ($process === false) {
    fwrite(STDERR, "Could not start {$binary}\n");
    exit(1);
}
exit(proc_close($process));

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

function isPhpScript(string $binary): bool
{
    if (str_ends_with($binary, '.php')) {
        return true;
    }
    $handle = @fopen($binary, 'r');
    if ($handle === false) {
        return false;
    }
    $firstLine = (string) fgets($handle);
    fclose($handle);

    return str_starts_with($firstLine, '#!') && str_contains($firstLine, 'php');
}
