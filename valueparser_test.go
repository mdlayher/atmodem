package atmodem

import "testing"

func Test_valueParserErrors(t *testing.T) {
	tests := []struct {
		name        string
		ss          []string
		fn          func(vp *valueParser)
		constructOK bool
	}{
		{
			name: "empty slice input",
		},
		{
			name: "bad float",
			ss:   []string{"foo"},
			fn: func(vp *valueParser) {
				_ = vp.Float64()
			},
			constructOK: true,
		},
		{
			name: "bad int",
			ss:   []string{"foo"},
			fn: func(vp *valueParser) {
				_ = vp.Int()
			},
			constructOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vp, err := newValueParser(tt.ss)
			if tt.constructOK && err != nil {
				t.Fatalf("failed to create valueParser: %v", err)
			}
			if !tt.constructOK && err == nil {
				t.Fatal("expected a constructor error, but none occurred")
			}
			if err != nil {
				t.Logf("construct err: %v", err)
				return
			}

			// Invoke the function and assume that any operation performed will return an error.
			tt.fn(vp)
			err = vp.Err()
			if err == nil {
				t.Fatal("expected non-nil vp.Err() error, but none occurred")
			}

			t.Logf("parse err: %v", err)
		})
	}
}
