<?php
namespace App\Services;
// Reset at the start of every request by App\Listeners\ResetIncompleteResetService (see config/octane.php)
class IncompleteResetService {
    private array $clean = [];
    private array $forgotten = [];
    public function addData(string $v): void { $this->clean[] = $v; $this->forgotten[] = $v; }
    public function getState(): array { return ['clean' => $this->clean, 'forgotten' => $this->forgotten]; }
    public function reset(): void { $this->clean = []; }
}
