<?php

namespace App\Controller;

use App\Entity\Invoice;
use Symfony\Component\HttpFoundation\Response;
use Symfony\Component\Routing\Attribute\Route;

class InvoiceController
{
    // The Invoice argument is not a service: Symfony registers an ".errored." placeholder for it
    #[Route('/invoice/{number}')]
    public function pay(Invoice $invoice): Response
    {
        $invoice->markPaid();

        return new Response(sprintf('Invoice %s paid: %s', $invoice->getNumber(), $invoice->isPaid() ? 'yes' : 'no'));
    }
}
