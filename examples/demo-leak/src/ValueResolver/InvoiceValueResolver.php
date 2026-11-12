<?php

namespace App\ValueResolver;

use App\Entity\Invoice;
use Symfony\Component\HttpFoundation\Request;
use Symfony\Component\HttpKernel\Controller\ValueResolverInterface;
use Symfony\Component\HttpKernel\ControllerMetadata\ArgumentMetadata;

// Builds the Invoice of the {number} route attribute, as Doctrine's EntityValueResolver loads an entity.
class InvoiceValueResolver implements ValueResolverInterface
{
    public function resolve(Request $request, ArgumentMetadata $argument): iterable
    {
        if (Invoice::class !== $argument->getType()) {
            return [];
        }

        return [new Invoice((string) $request->attributes->get('number'))];
    }
}
