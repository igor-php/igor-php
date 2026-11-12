<?php

namespace App\Entity;

// A data object, like a Doctrine entity: excluded from the services (config/services.yaml) and built
// for each request by InvoiceValueResolver. It never lives in the worker between two requests.
// Typing a controller argument with it makes Symfony register an ".errored." placeholder of this
// class in the controller's argument locator: Igor must not audit it as a shared service.
class Invoice
{
    private bool $paid = false;

    public function __construct(private string $number)
    {
    }

    public function getNumber(): string
    {
        return $this->number;
    }

    public function markPaid(): void
    {
        $this->paid = true;
    }

    public function isPaid(): bool
    {
        return $this->paid;
    }
}
