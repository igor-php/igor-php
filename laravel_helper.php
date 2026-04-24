<?php

/**
 * Igor Laravel Helper
 * This script extracts Octane configuration and maps classes to files.
 */

$root = $argv[1] ?? getcwd();
$results = [
    'flushed_classes' => [],
    'class_map' => []
];

// 1. Load Composer Autoloader
$autoload = $root . '/vendor/autoload.php';
if (file_exists($autoload)) {
    require_once $autoload;
}

// 2. Define shim for env() if it's not defined
if (!function_exists('env')) {
    function env($key, $default = null) {
        return $_ENV[$key] ?? $_SERVER[$key] ?? $default;
    }
}

// 3. Try to find Octane config
$octaneConfig = $root . '/config/octane.php';
if (file_exists($octaneConfig)) {
    try {
        // We include it in a closure to avoid variable pollution
        $loader = function($path) {
            return include $path;
        };
        $config = $loader($octaneConfig);
        if (is_array($config) && isset($config['flush'])) {
            $results['flushed_classes'] = $config['flush'];
        }
    } catch (\Throwable $e) { }
}

// 4. Scan app/ for class map
$appDir = $root . '/app';
if (is_dir($appDir)) {
    try {
        $iterator = new RecursiveIteratorIterator(new RecursiveDirectoryIterator($appDir));
        foreach ($iterator as $file) {
            if ($file->getExtension() === 'php') {
                $path = $file->getPathname();
                $content = file_get_contents($path);
                if (preg_match('/namespace\s+(.+?);/', $content, $nsMatch) &&
                    preg_match('/class\s+(.+?)\s+/', $content, $classMatch)) {
                    $fqcn = $nsMatch[1] . '\\' . $classMatch[1];
                    $results['class_map'][$fqcn] = realpath($path);
                }
            }
        }
    } catch (\Throwable $e) { }
}

echo json_encode($results);
