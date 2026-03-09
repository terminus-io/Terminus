package utils

import (
	"errors"
	"math/rand"
	"sync"
	"time"
)

var (
	once     sync.Once
	instance *IDPool
)

type IDPool struct {
	mu    sync.Mutex
	used  map[int]bool
	start int
	end   int
	rng   *rand.Rand
}

// GetProjectID 直接调用的导出函数
func GetProjectID() (int, error) {
	once.Do(func() {
		instance = &IDPool{
			used:  make(map[int]bool),
			start: 600000,
			end:   700000,
			rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
		}
	})

	instance.mu.Lock()
	defer instance.mu.Unlock()

	maxAttempts := instance.end - instance.start + 1
	if len(instance.used) >= maxAttempts {
		return 0, errors.New("project ID pool exhausted")
	}

	for {
		id := instance.rng.Intn(maxAttempts) + instance.start
		if !instance.used[id] {
			instance.used[id] = true
			return id, nil
		}
	}
}

func ReleaseProjectID(id int) {
	if instance == nil {
		return
	}
	instance.mu.Lock()
	defer instance.mu.Unlock()
	delete(instance.used, id)
}
