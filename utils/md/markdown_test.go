package md

import "testing"

func TestMarkDownToHTML(t *testing.T) {

	tests := []struct {
		name         string
		markdownText string
	}{
		{
			name: "normal",
			markdownText: `
# H1
hello world!**big hello world**
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkDownToHTML(tt.markdownText)
			t.Log("got:", got)
		})
	}
}
