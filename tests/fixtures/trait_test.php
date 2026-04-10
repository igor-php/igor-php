<?php

namespace App\Security;

trait UserAwareTrait
{
    private $user;

    public function setUser($user)
    {
        $this->user = $user;
    }
}
