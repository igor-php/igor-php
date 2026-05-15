<?php

function get_data() {
    // Superglobals (KO)
    $name = $_GET['name'];
    $post = $_POST['data'];
    $session = $_SESSION['user'];
    $cookie = $_COOKIE['session_id'];
    $request = $_REQUEST['any'];
    $env = $_ENV['APP_ENV'];
    
    // Local static variable (KO)
    static $counter = 0;
    return ++$counter;
}

class DangerousService {
    public function doSomething() {
        // More superglobals (KO)
        $server = $_SERVER['REQUEST_URI'];
        $files = $_FILES['upload'];
    }
}
