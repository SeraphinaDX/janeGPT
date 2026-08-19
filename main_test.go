package main

import "testing"

func TestStripEmailSignature(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "GrapheneOS",
			in:   "Explain why the sky is blue.\n\nSent from GrapheneOS\n",
			want: "Explain why the sky is blue.",
		},
		{
			name: "GrapheneOS case insensitive",
			in:   "Hello\r\n\r\nsent from grapheneos\r\n",
			want: "Hello",
		},
		{
			name: "standard signature delimiter",
			in:   "Question for the model\n\n-- \nBritney\nExample Company\n",
			want: "Question for the model",
		},
		{
			name: "iPhone signature",
			in:   "Summarize this.\n\nSent from my iPhone",
			want: "Summarize this.",
		},
		{
			name: "ordinary body unchanged",
			in:   "Tell me about GrapheneOS.\nIt is a mobile operating system.",
			want: "Tell me about GrapheneOS.\nIt is a mobile operating system.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripEmailSignature(tt.in); got != tt.want {
				t.Fatalf("stripEmailSignature() = %q, want %q", got, tt.want)
			}
		})
	}
}
