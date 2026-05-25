<?php
use Symfony\Contracts\Service\ResetInterface;

class UnsetService implements ResetInterface {
    private $customer;
    private $other;

    public function doSomething($val) {
        $this->customer = $val;
        $this->other = 'foo';
    }

    public function reset(): void {
        unset($this->customer);
        $this->other = null;
    }
}
