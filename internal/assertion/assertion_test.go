package assertion

// Compile-time check that every v0.1 primitive satisfies Assertion.
var (
	_ Assertion = StatusAssertion{}
	_ Assertion = UsageAssertion{}
	_ Assertion = (*SchemaAssertion)(nil)
)
