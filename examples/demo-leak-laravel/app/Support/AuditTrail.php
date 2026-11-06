<?php

namespace App\Support;

// Not a service: nothing in the container owns this class, yet its static array lives as long as the worker
class AuditTrail
{
    public static array $entries = [];
}
