package analyzer

// FrameworkLaravel names the framework whose wording replaces the Symfony one in hints.
const FrameworkLaravel = "Laravel"

const laravelSessionHint = "Native session functions bypass Laravel's session manager and leak session locks or data across requests in Worker mode. Use the session() helper or $request->session() instead."

// laravelHints replaces the Symfony advice of a rule, keyed by the rule (the function name for
// dangerous functions), when the audited project is a Laravel application.
var laravelHints = map[string]string{
	"set_error_handler":          "Custom error handlers persist across requests in Worker mode and bypass framework error handling. Report errors through Laravel's exception handler (withExceptions() in bootstrap/app.php) instead.",
	"set_exception_handler":      "Custom exception handlers persist across requests in Worker mode and bypass framework error handling. Register reporting and rendering callbacks with withExceptions() in bootstrap/app.php instead.",
	"register_shutdown_function": "Shutdown functions are deferred until worker process termination or accumulate across requests in Worker mode. Use a terminable middleware or an app()->terminating() callback instead.",
	"header":                     "Direct header output bypasses the Laravel response lifecycle and leaks into SAPI stream in Worker mode. Set headers on the response you return ($response->header($name, $value)) or in a middleware instead.",
	"http_response_code":         "Direct response code changes bypass the Laravel response lifecycle and persist across requests in Worker mode. Return a response with its status (response($content, $status)) or call abort() instead.",
	"session_start":              laravelSessionHint,
	"session_destroy":            laravelSessionHint,
	"session_id":                 laravelSessionHint,
	"exit":                       "Return a response or throw an exception (abort()) instead.",
	"superglobal":                "Use the Laravel Request object ($request->query(), $request->input(), the request() helper) instead.",
	"dependency_mutation":        "Avoid modifying injected dependencies at runtime, or give the dependency a reset() method called by an Octane RequestReceived listener.",
}

// SetFramework sets the framework of the audited project, which picks the wording of the hints.
func (v *PHPVisitor) SetFramework(framework string) {
	v.framework = framework
}

// hint returns the advice of a rule for the audited framework, the Symfony one by default.
func (v *PHPVisitor) hint(rule, symfony string) string {
	if v.framework == FrameworkLaravel {
		if h, ok := laravelHints[rule]; ok {
			return h
		}
	}
	return symfony
}
