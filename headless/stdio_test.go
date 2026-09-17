package headless

import "testing"

func TestParseArgs(t *testing.T) {
	for _, test := range []struct {
		name              string
		args              []string
		selected, invalid bool
		prompt, format    string
	}{
		{"tui", nil, false, false, "", "text"},
		{"acp", []string{"--acp"}, false, false, "", "text"},
		{"native", []string{"--headless"}, true, false, "", "stream-json"},
		{"shortcut", []string{"--stream-json"}, true, false, "", "stream-json"},
		{"prompt", []string{"-p", "hello", "--output-format", "stream-json"}, true, false, "hello", "text"},
		{"stream", []string{"-p", "--input-format=stream-json", "--output-format=stream-json", "--verbose", "--include-partial-messages"}, true, false, "", "stream-json"},
		{"unknown", []string{"--stream-json", "--unknown"}, true, true, "", ""},
		{"missing", []string{"--output-format"}, true, true, "", ""},
		{"format", []string{"--output-format", "text"}, true, true, "", ""},
		{"conflict", []string{"--stream-json", "-p", "hello"}, true, true, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			o, selected, err := ParseArgs(test.args)
			if selected != test.selected || (err != nil) != test.invalid {
				t.Fatalf("selected=%v err=%v", selected, err)
			}
			if !test.invalid && (o.Prompt != test.prompt || o.InputFormat != test.format) {
				t.Fatal(o)
			}
		})
	}
}
