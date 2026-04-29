<?php

namespace App\Http\Controllers;

use App\Services\StatefulService;
use App\Services\StaticLeakService;
use Illuminate\Http\Request;

class LeakDemoController extends Controller
{
    private $statefulService;
    private $staticLeakService;
    private $resettableService;

    public function __construct(
        StatefulService $statefulService,
        StaticLeakService $staticLeakService,
        \App\Services\ResettableService $resettableService
    ) {
        $this->statefulService = $statefulService;
        $this->staticLeakService = $staticLeakService;
        $this->resettableService = $resettableService;
    }

    public function index()
    {
        return $this->renderLayout("
            <h1>🧟 Igor Leak Lab (Laravel Edition)</h1>
            <div style='background: #fff3cd; padding: 15px; border: 1px solid #ffeeba; border-radius: 5px; margin-bottom: 20px;'>
                <b>⚠️ Important Note:</b> Everything you see is stored <b>exclusively in PHP's RAM</b>.
            </div>
            <p>Welcome, Master. Pick an experiment:</p>
            <ul>
                <li><a href='/stateful-service'>1. Stateful Service Leak</a></li>
                <li><a href='/resettable-service'>2. Octane Resettable Service (Safe)</a></li>
                <li><a href='/static-leak'>3. Static Property Leak</a></li>
                <li><a href='/stale-request'>4. Stale Request (Piège Octane)</a></li>
                <li><a href='/stale-config'>5. Stale Config (Piège Octane)</a></li>
                <li><a href='/check-timezone'>6. Global State Poisoning</a></li>
                <li><a href='/heavy-load' style='color: #dc3545; font-weight: bold;'>7. 🎮 CHALLENGE: Out of Memory Game</a></li>
                <li><a href='/exit'>8. The Danger of Exit/Die</a></li>
            </ul>
        ");
    }

    public function staleRequest()
    {
        $service = app(\App\Services\StaleRequestService::class);
        $currentPath = request()->path();
        $info = $service->getInfo();

        $html = "<h2>4. Stale Request</h2>
                 <p>In Octane, singletons are instantiated ONCE.</p>
                 <p>Current actual path: <b>/$currentPath</b></p>
                 <p>Path inside stale Request object: <b>/{$info['object_path']}</b></p>
                 <p>Internal Service Timestamp: <b>{$info['internal_timestamp']}</b> (should not change)</p>";

        if ($currentPath !== $info['object_path']) {
            $html .= "<div style='background: #f8d7da; color: #721c24; padding: 15px; border: 1px solid #f5c6cb; border-radius: 5px; margin-top: 20px;'>
                        🚩 <b>BUG DETECTED!</b><br>
                        The service is using a <b>stale Request object</b> from a previous execution.<br>
                        This is because it was injected in the constructor of a singleton.
                      </div>";
        } else {
            $html .= "<p style='color: #666;'><i>To see the bug: Go back to the home page, then come back here. The service will still think you are on the home page.</i></p>";
        }

        return $this->renderLayout($html, true);
    }

    public function staleConfig()
    {
        $service = app(\App\Services\StaleConfigService::class);
        $oldName = config('app.name');

        // Dynamically change config for this request
        config(['app.name' => 'Poisoned Name ' . time()]);
        $newName = config('app.name');
        $serviceName = $service->getAppName();

        $html = "<h2>5. Stale Config</h2>
                 <p>Current config app.name: <b>$newName</b></p>
                 <p>Service sees app.name: <b>$serviceName</b></p>";

        if ($serviceName !== $newName) {
            $html .= "<div style='color: #dc3545; padding: 10px; border: 1px solid; margin-top: 10px;'>
                        🚩 <b>BUG!</b> The singleton service is stuck with the initial configuration.
                      </div>";
        }

        return $this->renderLayout($html, true);
    }

    public function stateful()
    {
        $service = app(StatefulService::class);
        $service->addData('req_' . time() . '_' . rand(1,1000), 'I was here (Laravel)!');
        $html = "<h2>1. Stateful Service</h2>
                 <button onclick='window.location.reload()' style='padding: 10px 20px; background: #ff2d20; color: white; border: none; border-radius: 5px; cursor: pointer; font-weight: bold;'>➕ Add Data (F5)</button>
                 <pre style='background: #f8f9fa; padding: 10px; border: 1px solid #ddd; margin-top: 10px;'>".print_r($service->getData(), true)."</pre>";
        return $this->renderLayout($html, true);
    }

    public function resettable()
    {
        $service = app(\App\Services\ResettableService::class);
        $service->add('Request_' . time());
        $html = "<h2>2. Octane Resettable Service</h2>
                 <p style='color: #28a745; font-weight: bold;'>✅ This service is in Octane's 'flush' list.</p>
                 <button onclick='window.location.reload()' style='padding: 10px 20px; background: #28a745; color: white; border: none; border-radius: 5px; cursor: pointer; font-weight: bold;'>➕ Add Data (F5)</button>
                 <p>Data should <b>NOT</b> accumulate between refreshes:</p>
                 <pre style='background: #f8f9fa; padding: 10px; border: 1px solid #ddd; margin-top: 10px;'>".print_r($service->get(), true)."</pre>";
        return $this->renderLayout($html, true);
    }

    public function staticLeak()
    {
        $service = app(StaticLeakService::class);
        $service->touch('Laravel_User_' . rand(1, 100));
        $html = "<h2>3. Static Property Leak</h2>
                 <button onclick='window.location.reload()' style='padding: 10px 20px; background: #ff2d20; color: white; border: none; border-radius: 5px; cursor: pointer; font-weight: bold;'>🔄 Add Random User (F5)</button>
                 <pre style='background: #f8f9fa; padding: 10px; border: 1px solid #ddd; margin-top: 10px;'>".print_r($service->get(), true)."</pre>";
        return $this->renderLayout($html, true);
    }

    public function heavyLoad()
    {
        $this->statefulService->addData('heavy_' . uniqid(), str_repeat('L', 5 * 1024 * 1024));

        $limit = ini_get('memory_limit');
        $html = "<h2>4. 🎮 CHALLENGE: Out of Memory</h2>
                 <div style='background: #eee; padding: 20px; border-radius: 10px;'>
                    <p style='font-size: 1.2em; text-align: center;'>PHP Memory Limit: <b>$limit</b></p>
                    <div style='text-align: center;'>
                        <p>Each click injects <b>5MB</b> into the <code>StatefulService</code> cache array.</p>
                        <button onclick='window.location.reload()' style='padding: 20px 40px; background: #dc3545; color: white; border: none; border-radius: 10px; cursor: pointer; font-size: 1.5em; font-weight: bold; box-shadow: 0 4px #900;'>💥 BOOM! (Add 5MB)</button>
                    </div>
                 </div>";
        return $this->renderLayout($html, true);
    }

    public function checkTimezone()
    {
        $tz = date_default_timezone_get();
        $html = "<h2>3. Global Timezone</h2>
                 <p>Current process timezone: <b>$tz</b></p>
                 <a href='/poison-timezone' style='display: inline-block; padding: 10px 20px; background: #ffc107; color: black; text-decoration: none; border-radius: 5px; font-weight: bold;'>☣️ Inject America/New_York</a>";
        return $this->renderLayout($html, true);
    }

    public function poisonTimezone()
    {
        date_default_timezone_set('America/New_York');
        return "⚡ Poison injected! Timezone changed.";
    }

    public function exitDemo()
    {
        echo "💀 Worker Terminated (Laravel PID " . getmypid() . ")";
        exit();
    }

    private function renderLayout(string $content, bool $showBack = false)
    {
        $mem = number_format(memory_get_usage() / 1024 / 1024, 3);
        $peak = number_format(memory_get_peak_usage() / 1024 / 1024, 3);
        $back = $showBack ? "<br><br><a href='/' style='color: #666;'>⬅️ Back to Lab</a>" : "";

        $html = "
            <html>
            <body style='font-family: sans-serif; padding: 20px; line-height: 1.6;'>
                $content
                $back
                <hr style='margin-top: 50px;'>
                <div style='background: #333; color: #ff2d20; padding: 15px; font-family: monospace; border-radius: 5px; box-shadow: 0 4px 8px rgba(0,0,0,0.2);'>
                    <b>[ IGOR LARAVEL MONITOR ]</b><br>
                    <span style='color: #fff;'>Current RAM:</span> {$mem} MB<br>
                    <span style='color: #fff;'>Peak RAM:</span>    {$peak} MB<br>
                    <span style='color: #fff;'>Worker PID:</span>   " . getmypid() . "
                </div>
            </body>
            </html>";
        return response($html);
    }
}
