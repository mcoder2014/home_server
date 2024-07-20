package utils

import (
	"math/rand"
	"time"
)

func GenInt64ID() int64 {
	id := time.Now().UnixNano() << 24
	id += rand.Int63()%(2^24) - 1
	return id
}
