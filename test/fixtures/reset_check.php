<?php

namespace App\Service;

use Symfony\Contracts\Service\ResetInterface;

class IncompleteResetService implements ResetInterface
{
    private $user;
    private $tempData = [];

    public function handle($user)
    {
        $this->user = $user;
        $this->tempData[] = 'log';
    }

    public function reset(): void
    {
        $this->tempData = [];
    }
}

class CompleteResetService implements ResetInterface
{
    private $state;

    public function set($val) { $this->state = $val; }

    public function reset(): void
    {
        $this->state = null;
    }
}
