package auth

import "time"

// now returns the current time. It is a package variable so token-expiry checks
// can be exercised deterministically in tests.
var now = time.Now
