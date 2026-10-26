package main

import (
	"testing"

	"github.com/ebitengine/purego/objc"
)

func TestKeyRepeat(t *testing.T) {
	keyRepeat()
	key := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), "ApplePressAndHoldEnabled")
	defaults := objc.ID(objc.GetClass("NSUserDefaults")).Send(objc.RegisterName("standardUserDefaults"))
	if defaults.Send(objc.RegisterName("objectForKey:"), key) == 0 {
		t.Fatal("ApplePressAndHoldEnabled is not registered")
	}
	if objc.Send[bool](defaults, objc.RegisterName("boolForKey:"), key) {
		t.Fatal("ApplePressAndHoldEnabled is on: a key held down opens the accents")
	}
}
