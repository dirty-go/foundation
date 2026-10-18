package errors

import "testing"

func TestArgumentErrorError(t *testing.T) {
	cause := Msg("too long")
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"id and err", Arg("x", cause), "arg x: too long"},
		{"id and nil err", Arg("x", nil), "arg x invalid"},
		{"id and empty err", Arg("x", Msg("")), "arg x invalid"},
		{"no id and err", Arg("", cause), "arg too long"},
		{"no id and nil err", Arg("", nil), "invalid arg"},
		{"no id and empty err", Arg("", Msg("")), "invalid arg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}
