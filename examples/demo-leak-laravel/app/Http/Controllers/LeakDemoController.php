<?php

namespace App\Http\Controllers;

use App\Services\ClosureLeakService;
use App\Services\LocalStaticService;
use App\Services\FakeEntityManager;
use App\Services\IncompleteResetService;
use App\Services\StatefulService;
use App\Services\StaticLeakService;
use App\Services\StaleRequestService;
use App\Services\LazySingletonService;
use App\Services\DestructorLeakService;
use App\Services\ProcessStateLeakService;
use Illuminate\Http\Request;
use Symfony\Component\HttpFoundation\Response;

class LeakDemoController extends Controller
{
    public function __construct(
        private StatefulService $statefulService,
        private IncompleteResetService $incompleteResetService,
        private StaticLeakService $staticLeakService,
        private FakeEntityManager $entityManager,
        private ClosureLeakService $closureLeakService,
        private LocalStaticService $localStaticService,
        private DestructorLeakService $destructorLeakService,
        private ProcessStateLeakService $processStateLeakService,
        private StaleRequestService $staleRequestService,
        private LazySingletonService $lazySingletonService,
    ) {}

    public function index(): Response
    {
        return $this->renderLayout("
            <h1>🧟 Igor Laravel Leak Lab</h1>
            <div style='background: #fff3cd; padding: 15px; border: 1px solid #ffeeba; border-radius: 5px; margin-bottom: 20px;'>
                <b>⚠️ Important Note:</b> Everything you see is stored <b>exclusively in PHP's RAM</b>. 
            </div>
            <p>Welcome, Master. Pick an experiment:</p>
            <ul>
                <li><a href='/stateful-service'>1. Stateful Service Leak</a></li>
                <li><a href='/incomplete-reset'>2. Incomplete Reset Leak</a></li>
                <li><a href='/static-leak'>3. Static Property Leak</a></li>
                <li><a href='/check-timezone'>4. Global State Poisoning</a></li>
                <li><a href='/heavy-load' style='color: #dc3545; font-weight: bold;'>5. 🎮 CHALLENGE: Out of Memory Game</a></li>
                <li><a href='/exit'>6. The Danger of Exit/Die</a></li>
                <li><a href='/doctrine-leak' style='color: #28a745; font-weight: bold;'>7. Shared Service Indirect Mutation (Doctrine Filters Leak)</a></li>
                <li><a href='/closure-leak'>8. Closure State Leak</a></li>
                <li><a href='/local-static'>9. Local Static Variable</a></li>
                <li><a href='/superglobals'>10. PHP Superglobals</a></li>
                <li><a href='/destructor-leak' style='color: #fd7e14; font-weight: bold;'>11. Magic Method __destruct() Bypass (NEW)</a></li>
                <li><a href='/process-state-leak' style='color: #e83e8c; font-weight: bold;'>12. Dangerous Process State Mutations (NEW)</a></li>
                <li><a href='/stale-request?name=First' style='color: #6f42c1; font-weight: bold;'>13. Stale Request Captured by a Singleton (Laravel)</a></li>
                <li><a href='/lazy-singleton' style='color: #20c997; font-weight: bold;'>14. Lazy Singleton: Reset by the Octane Sandbox (Laravel)</a></li>
            </ul>
        ");
    }

    public function stateful(): Response
    {
        $this->statefulService->addData('req_' . time() . '_' . rand(1,1000), 'I was here!');
        $html = "<h2>1. Stateful Service</h2>
                 <button onclick='window.location.reload()' style='padding: 10px 20px; background: #007bff; color: white; border: none; border-radius: 5px; cursor: pointer; font-weight: bold;'>➕ Add Data (F5)</button>
                 <pre style='background: #f8f9fa; padding: 10px; border: 1px solid #ddd; margin-top: 10px;'>".print_r($this->statefulService->getData(), true)."</pre>";
                 
        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/stateful-service')\n" .
                          "public function stateful(): Response {\n" .
                          "    // Mutates the shared StatefulService by adding a new key on every request!\n" .
                          "    \$this->statefulService->addData('req_' . time() . '_' . rand(1, 1000), 'I was here!');\n" .
                          "}";
                          
        return $this->renderLayout($html, true, 'app/Services/StatefulService.php', $controllerCode);
    }

    public function incomplete(): Response
    {
        $this->incompleteResetService->addData('Value ' . time());
        $html = "<h2>2. Incomplete Reset</h2>
                 <button onclick='window.location.reload()' style='padding: 10px 20px; background: #007bff; color: white; border: none; border-radius: 5px; cursor: pointer; font-weight: bold;'>⚡ Trigger Request (F5)</button>
                 <pre style='background: #f8f9fa; padding: 10px; border: 1px solid #ddd; margin-top: 10px;'>".print_r($this->incompleteResetService->getState(), true)."</pre>";
                 
        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/incomplete-reset')\n" .
                          "public function incomplete(): Response {\n" .
                          "    // Mutates the shared IncompleteResetService on every request\n" .
                          "    \$this->incompleteResetService->addData('Value ' . time());\n" .
                          "}";
                          
        return $this->renderLayout($html, true, 'app/Services/IncompleteResetService.php', $controllerCode);
    }

    public function staticLeak(): Response
    {
        $this->staticLeakService->touch('User_' . rand(1, 100));
        $html = "<h2>3. Static Property Leak</h2>
                 <button onclick='window.location.reload()' style='padding: 10px 20px; background: #007bff; color: white; border: none; border-radius: 5px; cursor: pointer; font-weight: bold;'>🔄 Add Random User (F5)</button>
                 <pre style='background: #f8f9fa; padding: 10px; border: 1px solid #ddd; margin-top: 10px;'>".print_r($this->staticLeakService->get(), true)."</pre>";
                 
        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/static-leak')\n" .
                          "public function staticLeak(): Response {\n" .
                          "    // Calls a method that mutates a static array on the StaticLeakService\n" .
                          "    \$this->staticLeakService->touch('User_' . rand(1, 100));\n" .
                          "}";
                          
        return $this->renderLayout($html, true, 'app/Services/StaticLeakService.php', $controllerCode);
    }

    public function heavyLoad(): Response
    {
        // Allocate 5MB of random data to make it faster to crash
        $this->statefulService->addData('heavy_' . uniqid(), str_repeat('A', 5 * 1024 * 1024));
        
        $limit = ini_get('memory_limit');
        $html = "<h2>5. 🎮 CHALLENGE: Out of Memory</h2>
                 <div style='background: #eee; padding: 20px; border-radius: 10px;'>
                    <p style='font-size: 1.2em; text-align: center;'>PHP Memory Limit: <b>$limit</b></p>
                    
                    <div style='background: #fff; padding: 15px; border-left: 5px solid #dc3545; margin-bottom: 20px;'>
                        <b>💡 Real-world scenario:</b> Imagine this button is a <code>private array \$cache = []</code> in your service. 
                        Every time you fetch an entity from the database, you store it in this array to 'go faster'. 
                        But since you <b>never empty it</b> and the service <b>never dies</b>, your RAM will eventually explode.
                    </div>

                    <div style='text-align: center;'>
                        <p>Each click injects <b>5MB</b> into the <code>StatefulService</code> cache array.</p>
                        <button onclick='window.location.reload()' style='padding: 20px 40px; background: #dc3545; color: white; border: none; border-radius: 10px; cursor: pointer; font-size: 1.5em; font-weight: bold; box-shadow: 0 4px #900;'>💥 BOOM! (Add 5MB)</button>
                        <p style='margin-top: 20px; color: #666;'><i>Click until you see a 500 error. That's a memory leak in action!</i></p>
                    </div>
                 </div>";
                 
        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/heavy-load')\n" .
                          "public function heavyLoad(): Response {\n" .
                          "    // Injects 5MB of raw string into the StatefulService cache array on every request\n" .
                          "    \$this->statefulService->addData('heavy_' . uniqid(), str_repeat('A', 5 * 1024 * 1024));\n" .
                          "}";
                          
        return $this->renderLayout($html, true, 'app/Services/StatefulService.php', $controllerCode);
    }

    public function checkTimezone(): Response
    {
        $tz = date_default_timezone_get();
        $html = "<h2>4. Global Timezone</h2>
                 <p>Current process timezone: <b>$tz</b></p>
                 <a href='/poison-timezone' style='display: inline-block; padding: 10px 20px; background: #ffc107; color: black; text-decoration: none; border-radius: 5px; font-weight: bold;'>☣️ Inject America/New_York</a>
                 <a href='/check-timezone' style='display: inline-block; padding: 10px 20px; background: #6c757d; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; margin-left: 10px;'>🔍 Refresh Status</a>";
        
        $customCode = "<?php\n" .
                      "// Inside LeakDemoController.php:\n" .
                      "// Route::get('/poison-timezone')\n" .
                      "public function poisonTimezone(): Response {\n" .
                      "    // This modifies the global PHP process timezone for this worker thread forever!\n" .
                      "    date_default_timezone_set('America/New_York');\n" .
                      "}";
                      
        return $this->renderLayout($html, true, null, $customCode);
    }

    public function poisonTimezone(): Response
    {
        date_default_timezone_set('America/New_York');
        return new Response("
            <body style='font-family: sans-serif; padding: 20px; text-align: center; padding-top: 50px;'>
                " . $this->renderModeBanner() . "
                <h1>⚡ Poison injected!</h1>
                <p>Timezone changed for this worker thread.</p>
                <a href='/check-timezone' style='padding: 10px 20px; background: #28a745; color: white; text-decoration: none; border-radius: 5px; font-weight: bold;'>⬅️ Go back and check</a>
            </body>
        ");
    }

    public function exitDemo(): Response
    {
        echo "
            <body style='font-family: sans-serif; padding: 20px; text-align: center; padding-top: 50px;'>
                " . $this->renderModeBanner() . "
                <h1 style='color: #dc3545;'>💀 Worker Terminated</h1>
                <p>Process PID " . getmypid() . " was killed.</p>
                <a href='/' style='padding: 10px 20px; background: #007bff; color: white; text-decoration: none; border-radius: 5px; font-weight: bold;'>⬅️ Restart & Back to Lab</a>
            </body>";
        exit();
    }

    public function doctrineLeak(): Response
    {
        $isEnabled = $this->entityManager->getFilters()->isEnabled('softdeleteable');
        $statusStr = $isEnabled ? "<span style='color: #28a745; font-weight: bold;'>ENABLED (Safe)</span>" : "<span style='color: #dc3545; font-weight: bold;'>DISABLED (LEAKING!)</span>";
        
        $html = "<h2>7. Shared Service Indirect Mutation (Doctrine Filters)</h2>
                 <p>Doctrine softdeleteable filter is currently: $statusStr</p>
                 <div style='background: #f8f9fa; padding: 15px; border-left: 5px solid #007bff; margin-bottom: 20px;'>
                    <b>🧠 The Leak scenario:</b> Imagine a filter that resolves the <code>EntityManager</code> from the registry (into a local variable) 
                    and then disables the softdeleteable filter. Because the <code>EntityManager</code> is a shared service (Singleton), 
                    the filter is now disabled in the <b>entire process memory</b> for all subsequent requests!
                 </div>
                 <a href='/poison-filters' style='display: inline-block; padding: 10px 20px; background: #dc3545; color: white; text-decoration: none; border-radius: 5px; font-weight: bold;'>☣️ Disable Filter (via Local Variable)</a>
                 <a href='/doctrine-leak' style='display: inline-block; padding: 10px 20px; background: #6c757d; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; margin-left: 10px;'>🔍 Refresh Status</a>";
                 
        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/poison-filters')\n" .
                          "public function poisonFilters(): Response {\n" .
                          "    // Resolves the shared EntityManager singleton into a local variable\n" .
                          "    \$entityManager = \$this->entityManager;\n" .
                          "    // Indirectly mutates its state globally by disabling a filter!\n" .
                          "    \$entityManager->getFilters()->disable('softdeleteable');\n" .
                          "}";
                          
        return $this->renderLayout($html, true, 'app/Services/FakeEntityManager.php', $controllerCode);
    }

    public function poisonFilters(): Response
    {
        // We simulate resolving the entityManager into a local variable and mutating its shared state!
        $entityManager = $this->entityManager;
        $entityManager->getFilters()->disable('softdeleteable');
        
        return new Response("
            <body style='font-family: sans-serif; padding: 20px; text-align: center; padding-top: 50px;'>
                " . $this->renderModeBanner() . "
                <h1 style='color: #dc3545;'>⚡ Poison injected!</h1>
                <p>The softdeleteable filter has been disabled on the shared EntityManager singleton in RAM.</p>
                <a href='/doctrine-leak' style='padding: 10px 20px; background: #28a745; color: white; text-decoration: none; border-radius: 5px; font-weight: bold;'>⬅️ Go back and check the leak!</a>
            </body>
        ");
    }

    public function closureLeak(): Response
    {
        $v = 'State captured at ' . time();
        $this->closureLeakService->addListener('request', function () use ($v) {
            return $v;
        });

        $html = "<h2>8. Closure State Leak</h2>
                 <p>Listeners currently registered on the shared service: <b>" . count($this->closureLeakService->getListeners()['request'] ?? []) . "</b></p>
                 <div style='background: #f8f9fa; padding: 15px; border-left: 5px solid #007bff; margin-bottom: 20px;'>
                    <b>🧠 The Leak scenario:</b> We pass an anonymous function (closure) to a shared service. 
                    If this closure captures any local variables using the <code>use ()</code> clause, those variables and 
                    their whole context are kept alive in PHP's RAM as long as the shared service lives (forever in Worker mode)!
                 </div>
                 <button onclick='window.location.reload()' style='padding: 10px 20px; background: #007bff; color: white; border: none; border-radius: 5px; cursor: pointer; font-weight: bold;'>⚡ Trigger Request & Leak State (F5)</button>";
                 
        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/closure-leak')\n" .
                          "public function closureLeak(): Response {\n" .
                          "    \$v = 'State captured at ' . time();\n" .
                          "    // Passing a closure capturing local \$v to the shared ClosureLeakService listener array!\n" .
                          "    \$this->closureLeakService->addListener('request', function () use (\$v) {\n" .
                          "        return \$v;\n" .
                          "    });\n" .
                          "}";
                          
        return $this->renderLayout($html, true, 'app/Services/ClosureLeakService.php', $controllerCode);
    }

    public function localStatic(): Response
    {
        $calls = $this->localStaticService->incrementAndGet();

        $html = "<h2>9. Local Static Variable</h2>
                 <p>This service method has been called: <b>$calls</b> times across all requests on this worker thread.</p>
                 <div style='background: #f8f9fa; padding: 15px; border-left: 5px solid #007bff; margin-bottom: 20px;'>
                    <b>🧠 The Leak scenario:</b> Declaring a variable as <code>static</code> inside a method keeps its value alive 
                    across all executions of that method. Since the service instance lives forever in Worker mode, 
                    this local variable becomes a global state shared across all requests!
                 </div>
                 <button onclick='window.location.reload()' style='padding: 10px 20px; background: #007bff; color: white; border: none; border-radius: 5px; cursor: pointer; font-weight: bold;'>➕ Increment Call Count (F5)</button>";
                 
        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/local-static')\n" .
                          "public function localStatic(): Response {\n" .
                          "    // Calls incrementAndGet() which has an internal static variable!\n" .
                          "    \$calls = \$this->localStaticService->incrementAndGet();\n" .
                          "}";
                          
        return $this->renderLayout($html, true, 'app/Services/LocalStaticService.php', $controllerCode);
    }

    public function superglobals(): Response
    {
        if (isset($_GET['action']) && $_GET['action'] === 'poison-env') {
            $_ENV['APP_THEME'] = 'dark';
        }
        
        $theme = $_ENV['APP_THEME'] ?? 'light';
        $themeStatus = $theme === 'dark' ? "<span style='color: #dc3545; font-weight: bold;'>DARK (Poisoned!)</span>" : "<span style='color: #28a745; font-weight: bold;'>LIGHT (Clean)</span>";
        
        // Simulate reading directly from $_GET
        $name = $_GET['name'] ?? 'Stranger';
        
        $html = "<h2>10. PHP Superglobals Usage</h2>
                 <p>Hello, <b>" . htmlspecialchars($name, ENT_QUOTES, 'UTF-8') . "</b>!</p>
                 <p>Current APP_THEME inside <code>\$_ENV</code>: $themeStatus</p>
                 <div style='background: #f8f9fa; padding: 15px; border-left: 5px solid #007bff; margin-bottom: 20px;'>
                    <b>🧠 The Leak scenario:</b> Directly reading or writing legacy superglobals like <code>\$_GET</code> or <code>\$_ENV</code> is dangerous. 
                    While FrankenPHP resets <code>\$_GET</code> on every request, writing to <code>\$_ENV</code> (or calling <code>putenv()</code>) 
                    poisons the global process memory. All future requests will inherit this poisoned environment state!
                 </div>
                 <a href='/superglobals?action=poison-env' style='display: inline-block; padding: 10px 20px; background: #dc3545; color: white; text-decoration: none; border-radius: 5px; font-weight: bold;'>☣️ Write to \$_ENV['APP_THEME']</a>
                 <a href='/superglobals' style='display: inline-block; padding: 10px 20px; background: #6c757d; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; margin-left: 10px;'>🔍 Refresh Status</a>
                 
                 <form method='GET' action='/superglobals' style='margin-top: 25px;'>
                    <input type='text' name='name' placeholder='Type your name...' style='padding: 10px; border-radius: 5px; border: 1px solid #ccc;' required>
                    <button type='submit' style='padding: 10px 20px; background: #28a745; color: white; border: none; border-radius: 5px; font-weight: bold; cursor: pointer; margin-left: 10px;'>👋 Greet Me!</button>
                 </form>";
                 
        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/superglobals')\n" .
                          "public function superglobals(): Response {\n" .
                          "    // Writing to legacy \$_ENV superglobal permanently poisons the process!\n" .
                          "    if (isset(\$_GET['action']) && \$_GET['action'] === 'poison-env') {\n" .
                          "        \$_ENV['APP_THEME'] = 'dark';\n" .
                          "    }\n" .
                          "    \$theme = \$_ENV['APP_THEME'] ?? 'light';\n" .
                          "}";
                          
        return $this->renderLayout($html, true, null, $controllerCode);
    }

    public function destructorLeak(): Response
    {
        if (isset($_GET['action']) && $_GET['action'] === 'clear') {
            $this->destructorLeakService->clearLog();
            return redirect('/destructor-leak');
        }

        $logContent = $this->destructorLeakService->getLogContent();

        $html = "<h2>11. Magic Method __destruct() Bypass</h2>
                 <div style='background: #fff3cd; padding: 15px; border: 1px solid #ffeeba; border-radius: 5px; margin-bottom: 20px;'>
                    <b>💡 Destructor Dilemma in Worker mode:</b><br>
                    In worker mode, your services are instantiated once as singletons and stored in memory. 
                    Because the process survives between requests, <b>the destructor is never called</b>!<br>
                    Any resource cleanup (like closing files, databases, flushing buffers) written in <code>__destruct()</code> will never run per-request.
                 </div>
                 
                 <div style='background: #f8f9fa; padding: 15px; border-left: 5px solid #007bff; margin-bottom: 20px;'>
                    <b>🔍 The Experiment:</b><br>
                    1. Click <b>\"Clear Log File\"</b> below to start fresh.<br>
                    2. In <b>Classic mode (port 8081)</b>, refresh the page. Notice how every single refresh appends BOTH <code>🟢 Constructor called</code> AND <code>🔴 Destructor called</code> because PHP destroys the service at the end of every request.<br>
                    3. In <b>Worker mode (port 8080)</b>, refresh the page. Only <code>🟢 Constructor called</code> will appear (the very first time the worker starts), but <code>🔴 Destructor called</code> is **NEVER** called!
                 </div>

                 <div style='margin-bottom: 20px;'>
                    <button onclick='window.location.reload()' style='padding: 10px 20px; background: #007bff; color: white; border: none; border-radius: 5px; cursor: pointer; font-weight: bold;'>🔄 Refresh Request (F5)</button>
                    <a href='/destructor-leak?action=clear' style='display: inline-block; padding: 10px 20px; background: #dc3545; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; margin-left: 10px;'>🗑️ Clear Log File</a>
                 </div>
                 
                 <h3>📄 Life-cycle Event Log File (storage/destructor_demo.log):</h3>
                 <pre style='background: #1a202c; color: #f7fafc; padding: 15px; border-radius: 5px; font-family: monospace; font-size: 0.95em; overflow-x: auto;'>".htmlspecialchars($logContent, ENT_QUOTES, 'UTF-8')."</pre>";

        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/destructor-leak')\n" .
                          "public function destructorLeak(): Response {\n" .
                          "    // Read the log written to disk by __construct() and __destruct()\n" .
                          "    \$logContent = \$this->destructorLeakService->getLogContent();\n" .
                          "}";

        return $this->renderLayout($html, true, 'app/Services/DestructorLeakService.php', $controllerCode);
    }

    public function processStateLeak(Request $request): Response
    {
        $poison = $request->query->get('poison');
        $action = $request->query->get('action');

        $message = null;
        if ($poison === 'cwd') {
            $this->processStateLeakService->poisonCwd('/tmp');
            $message = "☣️ Injected: chdir('/tmp')";
        } elseif ($poison === 'umask') {
            $this->processStateLeakService->poisonUmask(0077);
            $message = "☣️ Injected: umask(0077)";
        } elseif ($poison === 'encoding') {
            $this->processStateLeakService->poisonEncoding('ISO-8859-1');
            $message = "☣️ Injected: mb_internal_encoding('ISO-8859-1')";
        } elseif ($poison === 'gc') {
            $this->processStateLeakService->poisonGc();
            $message = "☣️ Injected: gc_disable()";
        } elseif ($action === 'reset') {
            chdir('/app');
            umask(0022);
            mb_internal_encoding('UTF-8');
            gc_enable();
            $message = "🔄 Restored default process parameters";
        }

        $state = $this->processStateLeakService->getCurrentState();

        $alert = $message ? "<div style='background: #fff3cd; color: #856404; padding: 12px; border-radius: 5px; margin-bottom: 20px; font-weight: bold;'>$message</div>" : "";

        $html = "<h2>12. Dangerous Process State Mutations</h2>
                 $alert
                 <div style='background: #f8f9fa; padding: 15px; border-left: 5px solid #e83e8c; margin-bottom: 20px;'>
                    <b>🔍 The Experiment:</b><br>
                    Functions like <code>chdir()</code>, <code>umask()</code>, <code>mb_internal_encoding()</code>, or <code>gc_disable()</code> mutate process-wide C/PHP state.<br>
                    In <b>Classic Mode (port 8081)</b>, every request runs in a fresh process, so mutations disappear immediately on the next request.<br>
                    In <b>Worker Mode (port 8080)</b>, the worker process stays alive! Any mutation persists across subsequent requests, altering path lookups, file permissions, string encoding, or disabling the garbage collector entirely until OOM!
                 </div>

                 <div style='margin-bottom: 20px;'>
                    <a href='/process-state-leak' style='display: inline-block; padding: 10px 16px; background: #007bff; color: white; text-decoration: none; border-radius: 5px; font-weight: bold;'>🔄 Clean Refresh (F5)</a>
                    <a href='/process-state-leak?poison=cwd' style='display: inline-block; padding: 10px 16px; background: #dc3545; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; margin-left: 8px;'>☣️ chdir('/tmp')</a>
                    <a href='/process-state-leak?poison=umask' style='display: inline-block; padding: 10px 16px; background: #fd7e14; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; margin-left: 8px;'>☣️ umask(0077)</a>
                    <a href='/process-state-leak?poison=encoding' style='display: inline-block; padding: 10px 16px; background: #6f42c1; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; margin-left: 8px;'>☣️ mb_encoding('ISO-8859-1')</a>
                    <a href='/process-state-leak?poison=gc' style='display: inline-block; padding: 10px 16px; background: #d63384; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; margin-left: 8px;'>☣️ gc_disable()</a>
                    <a href='/process-state-leak?action=reset' style='display: inline-block; padding: 10px 16px; background: #28a745; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; margin-left: 8px;'>♻️ Reset Defaults</a>
                 </div>

                 <h3>📊 Current PHP Process State:</h3>
                 <pre style='background: #1a202c; color: #f7fafc; padding: 15px; border-radius: 5px; font-family: monospace; font-size: 1.05em; overflow-x: auto;'>".htmlspecialchars(print_r($state, true), ENT_QUOTES, 'UTF-8')."</pre>";

        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/process-state-leak')\n" .
                          "public function processStateLeak(Request \$request): Response {\n" .
                          "    // Calling chdir(), umask(), or gc_disable() poisons the entire worker process!\n" .
                          "    if (\$request->query->get('poison') === 'cwd') {\n" .
                          "        \$this->processStateLeakService->poisonCwd('/tmp');\n" .
                          "    }\n" .
                          "}";

        return $this->renderLayout($html, true, 'app/Services/ProcessStateLeakService.php', $controllerCode);
    }

    public function staleRequest(Request $request): Response
    {
        $captured = $this->staleRequestService->describe();
        $currentName = htmlspecialchars((string) $request->query('name', '(none)'), ENT_QUOTES, 'UTF-8');
        $capturedName = htmlspecialchars($captured['name'], ENT_QUOTES, 'UTF-8');
        $capturedUrl = htmlspecialchars($captured['url'], ENT_QUOTES, 'UTF-8');

        $html = "<h2>13. Stale Request Captured by a Singleton</h2>
                 <table style='border-collapse: collapse; margin-bottom: 20px;'>
                    <tr><td style='padding: 6px 12px;'><b>Current request</b> <code>?name=</code></td><td style='padding: 6px 12px;'>$currentName</td></tr>
                    <tr><td style='padding: 6px 12px;'><b>Request seen by the singleton</b> <code>?name=</code></td><td style='padding: 6px 12px;'>$capturedName</td></tr>
                    <tr><td style='padding: 6px 12px;'><b>URL seen by the singleton</b></td><td style='padding: 6px 12px;'><code>$capturedUrl</code></td></tr>
                 </table>
                 <div style='background: #f8f9fa; padding: 15px; border-left: 5px solid #6f42c1; margin-bottom: 20px;'>
                    <b>🧠 The Leak scenario:</b> <code>StaleRequestService</code> receives the <code>Request</code> in its constructor.
                    Octane builds it once when the worker boots (it is listed in <code>octane.warm</code>), so it keeps the request
                    that existed at that moment <b>forever</b>, while every new request gets its own fresh object.<br>
                    The same trap applies to the <code>Application</code> container and the <code>config</code> repository,
                    which is the first warning of the Laravel Octane documentation.<br>
                    In <b>Classic mode (port 8091)</b>, both lines always match.
                 </div>
                 <a href='/stale-request?name=Alice' style='display: inline-block; padding: 10px 20px; background: #6f42c1; color: white; text-decoration: none; border-radius: 5px; font-weight: bold;'>👤 ?name=Alice</a>
                 <a href='/stale-request?name=Bob' style='display: inline-block; padding: 10px 20px; background: #6f42c1; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; margin-left: 10px;'>👤 ?name=Bob</a>";

        $controllerCode = "<?php\n" .
                          "// Inside LeakDemoController.php:\n" .
                          "// Route::get('/stale-request')\n" .
                          "public function staleRequest(Request \$request): Response {\n" .
                          "    // The warmed singleton still holds the Request it received when it was built!\n" .
                          "    \$captured = \$this->staleRequestService->describe();\n" .
                          "}";

        return $this->renderLayout($html, true, 'app/Services/StaleRequestService.php', $controllerCode);
    }

    public function lazySingleton(): Response
    {
        $this->lazySingletonService->addData('req_' . time() . '_' . rand(1, 1000), 'I was here!');

        $html = "<h2>14. Lazy Singleton: Reset by the Octane Sandbox</h2>
                 <button onclick='window.location.reload()' style='padding: 10px 20px; background: #20c997; color: white; border: none; border-radius: 5px; cursor: pointer; font-weight: bold;'>➕ Add Data (F5)</button>
                 <pre style='background: #f8f9fa; padding: 10px; border: 1px solid #ddd; margin-top: 10px;'>" . print_r($this->lazySingletonService->getData(), true) . "</pre>
                 <div style='background: #f8f9fa; padding: 15px; border-left: 5px solid #20c997; margin-bottom: 20px;'>
                    <b>🧠 No leak here:</b> <code>LazySingletonService</code> has the same code as experiment 1 and is also registered with
                    <code>\$this->app->singleton()</code>, but it is <b>not</b> listed in <code>octane.warm</code>.
                    Octane clones the application for every request: this singleton is first built inside that clone and is thrown away
                    with it, so the array always holds a single entry.<br>
                    Compare with <a href='/stateful-service'>1. Stateful Service Leak</a>: what makes a Laravel service leak is
                    <b>when</b> it is first resolved (worker boot vs request), not only how it is bound.
                 </div>";

        $controllerCode = "<?php\n" .
                          "// Inside AppServiceProvider::register():\n" .
                          "\$this->app->singleton(StatefulService::class);      // + listed in octane.warm => leaks\n" .
                          "\$this->app->singleton(LazySingletonService::class); // not warmed => per request\n";

        return $this->renderLayout($html, true, 'app/Services/LazySingletonService.php', $controllerCode);
    }

    private function renderModeBanner(): string
    {
        [$label, $color] = match (strtolower($runtimeMode = (string) config('igor.runtime_mode'))) {
            'worker' => ['WORKER (Laravel Octane)', '#dc3545'],
            'classic' => ['CLASSIC', '#28a745'],
            default => [strtoupper($runtimeMode), '#6c757d'],
        };

        return "
            <div style='background: $color; color: white; padding: 10px 15px; border-radius: 5px; margin-bottom: 20px; font-family: sans-serif; text-align: left;'>
                <b>⚙️ FrankenPHP mode: $label</b>
            </div>";
    }

    private function renderLayout(string $content, bool $showBack = false, ?string $codeFile = null, ?string $customCode = null): Response
    {
        $mem = number_format(memory_get_usage() / 1024 / 1024, 3);
        $peak = number_format(memory_get_peak_usage() / 1024 / 1024, 3);
        $back = $showBack ? "<br><br><a href='/' style='color: #666;'>⬅️ Back to Lab</a>" : "";
        
        $codeBoxes = [];
        if ($codeFile !== null) {
            $fullPath = __DIR__ . '/../../../' . $codeFile;
            if (file_exists($fullPath)) {
                $codeContent = file_get_contents($fullPath);
                $escapedCode = htmlspecialchars($codeContent, ENT_QUOTES, 'UTF-8');
                $codeBoxes[] = "
                    <div style='margin-top: 30px; box-shadow: 0 4px 12px rgba(0,0,0,0.15); border-radius: 8px; overflow: hidden; border: 1px solid #e2e8f0;'>
                        <div style='background: #2d3748; color: #cbd5e0; padding: 10px 15px; font-family: monospace; font-size: 0.85em; font-weight: bold; border-bottom: 1px solid #4a5568;'>
                            📂 $codeFile (Service Implementation)
                        </div>
                        <pre style='background: #1a202c; color: #f7fafc; padding: 20px; margin: 0; overflow-x: auto; font-family: monospace; font-size: 0.9em; line-height: 1.5;'>$escapedCode</pre>
                    </div>";
            }
        }
        if ($customCode !== null) {
            $escapedCode = htmlspecialchars($customCode, ENT_QUOTES, 'UTF-8');
            $codeBoxes[] = "
                <div style='margin-top: 30px; box-shadow: 0 4px 12px rgba(0,0,0,0.15); border-radius: 8px; overflow: hidden; border: 1px solid #e2e8f0;'>
                    <div style='background: #2d3748; color: #cbd5e0; padding: 10px 15px; font-family: monospace; font-size: 0.85em; font-weight: bold; border-bottom: 1px solid #4a5568;'>
                        ⚡ Controller Call Pattern
                    </div>
                    <pre style='background: #1a202c; color: #f7fafc; padding: 20px; margin: 0; overflow-x: auto; font-family: monospace; font-size: 0.9em; line-height: 1.5;'>$escapedCode</pre>
                </div>";
        }
        $codeBoxHtml = implode('', $codeBoxes);

        $html = "
            <html>
            <body style='font-family: sans-serif; padding: 20px; line-height: 1.6;'>
                {$this->renderModeBanner()}
                $content 
                $codeBoxHtml
                $back 
                <hr style='margin-top: 50px;'>
                <div style='background: #333; color: #0f0; padding: 15px; font-family: monospace; border-radius: 5px; box-shadow: 0 4px 8px rgba(0,0,0,0.2);'>
                    <b>[ IGOR MEMORY MONITOR ]</b><br>
                    <span style='color: #fff;'>Current RAM:</span> {$mem} MB<br>
                    <span style='color: #fff;'>Peak RAM:</span>    {$peak} MB<br>
                    <span style='color: #fff;'>Worker PID:</span>   " . getmypid() . "
                </div>
            </body>
            </html>";
        return new Response($html);
    }
}
