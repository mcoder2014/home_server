package md

import (
	"html/template"

	"github.com/russross/blackfriday/v2"
)

func MarkDownToHTML(markdownText string) string {
	markdownText = template.HTMLEscapeString(markdownText)
	return string(blackfriday.Run([]byte(markdownText)))
}
