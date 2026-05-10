//go:build darwin

package session

import "testing"

func TestParseEnvVarFromPS(t *testing.T) {
	tests := []struct {
		name  string
		input string
		key   string
		want  string
	}{
		{
			name:  "iterm2_term_program",
			input: "  PID TTY           TIME CMD\n12345 s002         0:01.23 -zsh TERM_PROGRAM=iTerm.app TERM=xterm-256color\n",
			key:   "TERM_PROGRAM",
			want:  "iTerm.app",
		},
		{
			name:  "apple_terminal_term_program",
			input: "  PID TTY           TIME CMD\n12345 s002         0:01.23 -zsh TERM_PROGRAM=Apple_Terminal TERM=xterm-256color\n",
			key:   "TERM_PROGRAM",
			want:  "Apple_Terminal",
		},
		{
			name:  "ghostty_term_program",
			input: "  PID TTY           TIME CMD\n12345 s002         0:01.23 -zsh TERM_PROGRAM=ghostty TERM=xterm-ghostty\n",
			key:   "TERM_PROGRAM",
			want:  "ghostty",
		},
		{
			name:  "not_present",
			input: "  PID TTY           TIME CMD\n12345 s002         0:01.23 -zsh TERM=xterm-256color\n",
			key:   "TERM_PROGRAM",
			want:  "",
		},
		{
			name:  "empty",
			input: "",
			key:   "TERM_PROGRAM",
			want:  "",
		},
		{
			name:  "at_end_of_line",
			input: "... TERM_PROGRAM=iTerm.app\n",
			key:   "TERM_PROGRAM",
			want:  "iTerm.app",
		},
		{
			name:  "last_field_no_newline",
			input: "... TERM_PROGRAM=Apple_Terminal",
			key:   "TERM_PROGRAM",
			want:  "Apple_Terminal",
		},
		{
			name:  "term_extraction_for_kitty_fallback",
			input: "... TERM=xterm-kitty TERM_PROGRAM=\n",
			key:   "TERM",
			want:  "xterm-kitty",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseEnvVarFromPS(tc.input, tc.key)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
