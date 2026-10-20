// Package optional — поля частичного обновления (PATCH): отличает «поле не передано»
// от «передан null» и от значения.
package optional

import (
	"bytes"
	"encoding/json"
)

// Field — значение из JSON. Set=false — поля не было в запросе;
// Set=true и Value=nil — передан null; иначе Value — переданное значение.
type Field[T any] struct {
	Set   bool
	Value *T
}

// Of — поле, переданное со значением v.
func Of[T any](v T) Field[T] { return Field[T]{Set: true, Value: &v} }

// Null — поле, переданное как null.
func Null[T any]() Field[T] { return Field[T]{Set: true} }

// UnmarshalJSON вызывается только для присутствующих в JSON полей.
func (f *Field[T]) UnmarshalJSON(b []byte) error {
	f.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		f.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	f.Value = &v
	return nil
}

// Apply возвращает новое значение поля, если оно передано, иначе cur.
func (f Field[T]) Apply(cur *T) *T {
	if !f.Set {
		return cur
	}
	return f.Value
}
