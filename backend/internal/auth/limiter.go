package auth

import (
	"sync"
	"time"
)

// Limiter считает неудачные попытки по ключам в скользящем окне. Хранит всё в памяти:
// для MVP с одним экземпляром сервера этого достаточно, после рестарта счётчики обнуляются.
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	fails     map[string][]time.Time
	lastSweep time.Time
}

// NewLimiter — не больше max неудач за window на ключ. now == nil — time.Now.
func NewLimiter(max int, window time.Duration, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{max: max, window: window, now: now, fails: map[string][]time.Time{}}
}

// Blocked сообщает, исчерпан ли лимит хотя бы по одному ключу, и через сколько можно повторить.
func (l *Limiter) Blocked(keys ...string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)

	var retry time.Duration
	for _, k := range keys {
		f := l.recent(k, now)
		if len(f) >= l.max {
			// Повторить можно, когда из окна выйдет самая старая из последних max неудач.
			if d := f[len(f)-l.max].Add(l.window).Sub(now); d > retry {
				retry = d
			}
		}
	}
	return retry > 0, retry
}

// Fail записывает неудачную попытку по каждому ключу.
func (l *Limiter) Fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for _, k := range keys {
		l.fails[k] = append(l.recent(k, now), now)
	}
}

// Reset очищает счётчик ключа (например, телефона после успешного входа).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

// recent возвращает неудачи ключа внутри окна. Вызывать под mu.
func (l *Limiter) recent(key string, now time.Time) []time.Time {
	f := l.fails[key]
	i := 0
	for i < len(f) && now.Sub(f[i]) >= l.window {
		i++
	}
	if i == len(f) {
		delete(l.fails, key)
		return nil
	}
	f = f[i:]
	l.fails[key] = f
	return f
}

// sweep раз в окно удаляет устаревшие ключи, чтобы карта не росла бесконечно. Вызывать под mu.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now
	for k := range l.fails {
		l.recent(k, now)
	}
}
