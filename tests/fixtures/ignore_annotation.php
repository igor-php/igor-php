<?php

namespace App\Service;

class IgnoredService {
    private $cache = [];

    public function getData($val) {
        // This should be reported
        $this->cache[] = $val;

        // This should be ignored (same line)
        $this->cache[] = $val; // @igor-ignore

        // This should be ignored (previous line)
        // @igor-ignore
        $this->cache[] = $val;
        
        // Superglobal ignored
        $user = $_GET['user']; // @igor-ignore
        
        // Exit ignored
        // @igor-ignore
        exit;
    }
}
