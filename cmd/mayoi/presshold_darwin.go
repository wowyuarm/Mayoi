package main

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// keyRepeat has a key held down repeat, in the terminal and the editor,
// rather than open the accents of a letter, as in Ghostty and iTerm2: a
// default under the user's own, so that `defaults write dev.mayoi.editor
// ApplePressAndHoldEnabled -bool true` gives the accents back.
func keyRepeat() {
	if _, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_GLOBAL); err != nil {
		return
	}
	str := func(s string) objc.ID {
		return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), s)
	}
	no := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithBool:"), false)
	defaults := objc.ID(objc.GetClass("NSDictionary")).Send(objc.RegisterName("dictionaryWithObject:forKey:"), no, str("ApplePressAndHoldEnabled"))
	objc.ID(objc.GetClass("NSUserDefaults")).Send(objc.RegisterName("standardUserDefaults")).
		Send(objc.RegisterName("registerDefaults:"), defaults)
}
