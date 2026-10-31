// Package assistant is Mango, the in-app AI assistant.
//
// The app talks only to this API. The API checks the user, the notice,
// limits and the input, then calls the internal greenroot-ai service, which
// alone holds the AI provider key. Business data reaches the model only
// through the tools here: each tool calls an existing API endpoint as the
// signed-in user (same token, same permission and nursery checks as the app)
// and passes on an allowlist of fields. Mango changes nothing by itself: a
// change is stored as a proposal and runs through the normal endpoint only
// after the user taps Confirm.
//
// Nothing the user says is stored: no audio, transcript or reply. Usage rows
// keep units and an estimated cost for limits and the budget.
package assistant
