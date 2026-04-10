<?php
// find_class_files.php
error_reporting(0);
ini_set('display_errors', 0);

$projectRoot = $argv[1] ?? '.';
require_once $projectRoot . '/vendor/autoload.php';

$json = file_get_contents('php://stdin');
$data = json_decode($json, true);

$mapping = [];
foreach ($data['definitions'] as $id => $def) {
    $class = $def['class'] ?? null;
    if (!$class || !($def['shared'] ?? true)) continue;

    try {
        if (class_exists($class) || interface_exists($class) || trait_exists($class)) {
            $reflection = new ReflectionClass($class);
            $file = $reflection->getFileName();
            if ($file && file_exists($file)) {
                $mapping[$class] = $file;
            }
        }
    } catch (Throwable $e) {
    }
}

echo json_encode($mapping);
