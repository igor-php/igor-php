<?php

namespace IgorPhp\IgorBundle\Runtime\Report;

use IgorPhp\IgorBundle\Runtime\LeakReport;

interface LeakReporterInterface
{
    public function report(LeakReport $report): void;
}
