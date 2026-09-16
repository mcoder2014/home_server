package webcomments

import (
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/utils"
)

func validCommentInput() Input {
	return Input{
		RequestID: "request-1",
		ReleaseID: "10",
		PageKey:   "id:guide-page",
		PagePath:  "index.html",
		Anchor: Anchor{
			Kind:   "text",
			Exact:  "需要核对的原文",
			PageID: "guide-page",
		},
		Body: "请核对这里",
	}
}

func TestValidateAcceptsSupportedCommentAnchor(t *testing.T) {
	if err := Validate(validCommentInput(), "comment"); err != nil {
		t.Fatalf("valid comment rejected: %v", err)
	}
}

func TestValidateUsesRuneLimitsForUserText(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Input)
	}{
		{name: "body over 4000 runes", change: func(input *Input) { input.Body = string(make([]rune, 4001)) }},
		{name: "quote over 4096 runes", change: func(input *Input) { input.Anchor.Exact = string(make([]rune, 4097)) }},
		{name: "prefix over 128 runes", change: func(input *Input) { input.Anchor.Prefix = string(make([]rune, 129)) }},
		{name: "suffix over 128 runes", change: func(input *Input) { input.Anchor.Suffix = string(make([]rune, 129)) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validCommentInput()
			test.change(&input)
			if err := Validate(input, "comment"); err == nil {
				t.Fatal("oversized input accepted")
			}
		})
	}
}

func TestValidateRejectsAmbiguousOrUnsafeAnchors(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Input)
	}{
		{name: "unstable target id", change: func(input *Input) { input.Anchor.TargetID = "bad_ID" }},
		{name: "path traversal", change: func(input *Input) { input.PagePath = "../private.html" }},
		{name: "unmatched page key", change: func(input *Input) { input.PageKey = "id:another-page" }},
		{name: "image without stable target", change: func(input *Input) { input.Anchor = Anchor{Kind: "image", PageID: "guide-page"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validCommentInput()
			test.change(&input)
			if err := Validate(input, "comment"); err == nil {
				t.Fatal("unsafe anchor accepted")
			}
		})
	}
}

func TestActorNameSnapshotSupportsLegacyIdentityMode(t *testing.T) {
	before := config.Global()
	t.Cleanup(func() { config.SetGlobalConfig(before) })
	config.SetGlobalConfig(config.Config{IdentitySource: "file"})

	name, err := actorNameSnapshot(nil, &utils.Principal{Kind: "user", UserID: 42})
	if err != nil || name != "用户 42" {
		t.Fatalf("legacy identity snapshot = %q, %v", name, err)
	}
}
