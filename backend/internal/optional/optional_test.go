package optional

import (
	"encoding/json"
	"testing"
)

func TestFieldUnmarshal(t *testing.T) {
	type req struct {
		Count Field[int32]  `json:"count"`
		Name  Field[string] `json:"name"`
	}
	tests := []struct {
		in        string
		set, null bool
		want      int32
	}{
		{`{}`, false, false, 0},
		{`{"count": null}`, true, true, 0},
		{`{"count": 12}`, true, false, 12},
		{`{"count": 0}`, true, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			var r req
			if err := json.Unmarshal([]byte(tt.in), &r); err != nil {
				t.Fatal(err)
			}
			if r.Count.Set != tt.set || (r.Count.Value == nil) != (tt.null || !tt.set) {
				t.Fatalf("Count = %+v", r.Count)
			}
			if r.Count.Value != nil && *r.Count.Value != tt.want {
				t.Errorf("Count = %d, want %d", *r.Count.Value, tt.want)
			}
			if r.Name.Set {
				t.Error("Name.Set без поля name")
			}
		})
	}

	var r req
	if err := json.Unmarshal([]byte(`{"count": "x"}`), &r); err == nil {
		t.Error("ожидалась ошибка типа")
	}
}

func TestFieldApply(t *testing.T) {
	cur := int32(5)
	if got := (Field[int32]{}).Apply(&cur); got == nil || *got != 5 {
		t.Errorf("не передано: %v", got)
	}
	if got := Null[int32]().Apply(&cur); got != nil {
		t.Errorf("null: %v", *got)
	}
	if got := Of[int32](7).Apply(&cur); got == nil || *got != 7 {
		t.Errorf("значение: %v", got)
	}
}
