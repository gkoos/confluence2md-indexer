package indexer

import (
	"encoding/json"
	"testing"
)

func TestPageIDUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
		wantErr bool
	}{
		{name: "number", payload: `12345`, want: "12345"},
		{name: "numeric string", payload: `"12345"`, want: "12345"},
		{name: "padded number", payload: " 12345 ", want: "12345"},
		{name: "padded string", payload: ` " 12345 " `, want: "12345"},
		{name: "null", payload: `null`, want: ""},
		{name: "empty string", payload: `""`, want: ""},
		{name: "float is rejected", payload: `1.5`, wantErr: true},
		{name: "object is rejected", payload: `{}`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got pageID

			err := json.Unmarshal([]byte(test.payload), &got)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %s, got %q", test.payload, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unmarshal %s: %v", test.payload, err)
			}
			if got.String() != test.want {
				t.Fatalf("page id = %q, want %q", got.String(), test.want)
			}
		})
	}
}
