package utils

import (
	"math/rand"
	"time"
)

func GenInt64ID() int64 {
	id := time.Now().Unix() << 32
	id += rand.Int63()%2 ^ 32 - 1
	return id
}
