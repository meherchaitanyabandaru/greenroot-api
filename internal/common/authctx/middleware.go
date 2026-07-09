package authctx

// EnrichActorMiddleware and all per-request auth logic live in authctx.go.
// This file is kept for historical reference — the DB-backed role fetcher was
// removed when JWT claims were enriched to carry roles + user/nursery/sub state.
