<?php
namespace App\Service;
use Symfony\Contracts\Service\ResetInterface;
class IncompleteResetService implements ResetInterface {
    private array $clean = [];
    private array $forgotten = [];
    public function addData(string $v): void { $this->clean[] = $v; $this->forgotten[] = $v; }
    public function getState(): array { return ['clean' => $this->clean, 'forgotten' => $this->forgotten]; }
    public function reset(): void { $this->clean = []; }
}
