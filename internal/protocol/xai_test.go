package protocol

import "testing"

func TestJSSourceRegexAllowsTurbopackQuery(t *testing.T) {
	html := `<script async src="/_next/static/chunks/signup.js?dpl=abc" nonce="token"></script>`
	matches := jsSrcRe.FindAllStringSubmatch(html, -1)
	if len(matches) != 1 || matches[0][1] != "/_next/static/chunks/signup.js" {
		t.Fatalf("script source mismatch: %#v", matches)
	}
}

func TestActionIDFromJSPrefersSignupMutationReference(t *testing.T) {
	trackingAction := "111111111111111111111111111111111111111111"
	signupAction := "222222222222222222222222222222222222222222"
	js := `createUser; tracked=(0,a.createServerReference)("` + trackingAction + `");submit=(0,a.createServerReference)("` + signupAction + `");useMutation({mutationFn:submit})`
	if got := actionIDFromJS(js); got != signupAction {
		t.Fatalf("action id=%q want %q", got, signupAction)
	}
}

func TestActionIDFromJSFallsBackToFirstReference(t *testing.T) {
	action := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	js := `registerUser;(0,a.createServerReference)("` + action + `")`
	if got := actionIDFromJS(js); got != action {
		t.Fatalf("action id=%q want %q", got, action)
	}
}
