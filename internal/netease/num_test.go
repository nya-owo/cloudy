package netease

import (
	"encoding/json"
	"testing"
)

func TestNum(t *testing.T) {
	var tr Track
	if err := json.Unmarshal([]byte(`{"id":1,"no":3,"cd":"02"}`), &tr); err != nil || tr.No != 3 || tr.Cd != 2 {
		t.Fatalf("%v %+v", err, tr)
	}
	if err := json.Unmarshal([]byte(`{"id":1,"no":"x","cd":null}`), &tr); err != nil || tr.No != 0 || tr.Cd != 0 {
		t.Fatalf("%v %+v", err, tr)
	}
	b, _ := json.Marshal(Track{ID: 1, No: 4})
	var back Track
	if err := json.Unmarshal(b, &back); err != nil || back.No != 4 {
		t.Fatalf("%s %v %+v", b, err, back)
	}
}
