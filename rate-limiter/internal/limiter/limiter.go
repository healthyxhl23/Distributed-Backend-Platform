package limiter

import "github.com/herryxhl/rate-limiter/internal/ratelimit"

// Result and Limiter are defined in the ratelimit package to avoid an import
// cycle between limiter and store. These aliases keep the public API unchanged.
type Result = ratelimit.Result
type Limiter = ratelimit.Limiter