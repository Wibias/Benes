// Package sessions is the durable Sessions v1 observability store.
//
// Diagnostics logs stay a separate in-memory ring; this package never stores
// prompts, responses, or credentials. List search and session aggregates are
// SQL-side and must not load the full 30-day history into Go.
package sessions
