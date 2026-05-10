//go:build darwin

package session

import "testing"

func TestParseTermProgramFromPS(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "iterm2",
			input: "  PID TTY           TIME CMD\n12345 s002         0:01.23 -zsh TERM_PROGRAM=iTerm.app TERM=xterm-256color\n",
			want:  "iTerm.app",
		},
		{
			name:  "apple_terminal",
			input: "  PID TTY           TIME CMD\n12345 s002         0:01.23 -zsh TERM_PROGRAM=Apple_Terminal TERM=xterm-256color\n",
			want:  "Apple_Terminal",
		},
		{
			name:  "ghostty",
			input: "  PID TTY           TIME CMD\n12345 s002         0:01.23 -zsh TERM_PROGRAM=ghostty TERM=xterm-ghostty\n",
			want:  "ghostty",
		},
		{
			name:  "not_present",
			input: "  PID TTY           TIME CMD\n12345 s002         0:01.23 -zsh TERM=xterm-256color\n",
			want:  "",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "at_end_of_line",
			input: "... TERM_PROGRAM=iTerm.app\n",
			want:  "iTerm.app",
		},
		{
			name:  "last_field_no_newline",
			input: "... TERM_PROGRAM=Apple_Terminal",
			want:  "Apple_Terminal",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTermProgramFromPS(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
