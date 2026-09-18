package utils

import (
	"sync"
	"time"
)

var idGenerator struct {
	sync.Mutex
	last int64
}

// GenInt64ID returns positive, process-local monotonic IDs. Nanosecond time
// fits int64 without shifting; the lock also handles concurrent calls and
// clocks moving backwards. Exhaustion fails before returning an invalid ID.
func GenInt64ID() int64 {
	idGenerator.Lock()
	defer idGenerator.Unlock()
	id := time.Now().UnixNano()
	if id <= idGenerator.last {
		id = idGenerator.last + 1
	}
	if id <= 0 {
		panic("positive int64 ID space exhausted")
	}
	idGenerator.last = id
	return id
}
