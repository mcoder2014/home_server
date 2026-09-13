package md

import "testing"

// TestMarkDownToHTML 将含标题和加粗文本的 Markdown 转换成 HTML，记录结果供人工检查。
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
