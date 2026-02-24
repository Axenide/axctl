package client
import "sync"
type syncMap[K comparable, V any] struct {
	m sync.Map
}
func (m *syncMap[K, V]) Load(key K) (value V, ok bool) {
	v, ok := m.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return v.(V), true
}
func (m *syncMap[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}
func (m *syncMap[K, V]) Delete(key K) {
	m.m.Delete(key)
}
func (m *syncMap[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(k, v interface{}) bool {
		return f(k.(K), v.(V))
	})
}
