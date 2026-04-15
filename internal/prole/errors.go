package prole

import "errors"

// ErrMaxProlesLimitReached is returned by Create when the max_proles cap has
// been reached and a new prole cannot be created.
var ErrMaxProlesLimitReached = errors.New("max_proles limit reached")
