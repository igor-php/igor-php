<?php

namespace App\Service;

class ProcessService
{
    public function mutateFilesystem(string $dir)
    {
        chdir($dir);
        \chdir($dir);
        umask(0022);
    }

    public function mutateEncoding()
    {
        mb_internal_encoding('UTF-8');
        mb_regex_encoding('UTF-8');
    }

    public function mutateMath()
    {
        bcscale(4);
    }

    public function mutateHandlers()
    {
        set_error_handler(function () {});
        set_exception_handler(function () {});
        register_shutdown_function(function () {});
    }

    public function mutateGc()
    {
        gc_disable();
    }

    public function mutateHttp()
    {
        header('X-Custom: 1');
        http_response_code(200);
    }

    public function mutateSession()
    {
        session_start();
        session_id('custom_id');
        session_destroy();
    }

    public function ignoredMutations()
    {
        // @igor-ignore
        chdir('/tmp');
        // @igor-ignore
        gc_disable();
    }

    public function safeGetters()
    {
        $encoding = mb_internal_encoding();
        $regex = mb_regex_encoding();
        $scale = bcscale();
        $code = http_response_code();
        $sess = session_id();
        $mask = umask();
    }
}
