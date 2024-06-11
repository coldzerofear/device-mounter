package lock

import "sync"

var (
	mapLock sync.Map
)

type lockItem struct {
	ch chan struct{}
}

func setnx(key any) bool {
	_, loaded := mapLock.LoadOrStore(key, struct{}{})
	return !loaded
}

func TryLock(key any) bool {
	return setnx(key)
}

func Unlock(key any) {
	item, loaded := mapLock.LoadAndDelete(key)
	if loaded {
		if li, ok := item.(*lockItem); ok {
			close(li.ch) // 通知等待的goroutine
		}
	}
}

func Lock(key any) {
	item := lockItem{ch: make(chan struct{})}
loop:
	it, loaded := mapLock.LoadOrStore(key, &item)
	if loaded {
		if li, ok := it.(*lockItem); ok {
			<-li.ch // 阻塞等待
		}
		goto loop
	}
}
