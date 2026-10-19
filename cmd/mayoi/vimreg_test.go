package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestVimMacros(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "one\ntwo\nthree\nfour\nfive\n")
	hs.vim("qa")
	if !hs.tt.HasText("recording @a") {
		t.Fatal("the status bar does not say a register records")
	}
	// The text typed in insert mode records as it is.
	hs.vim("A")
	hs.insert(";")
	hs.vim("<Esc>jq")
	if hs.tt.HasText("recording @a") {
		t.Fatal("q did not stop the recording")
	}
	if got := hs.a.vimRegs['a'].text; got != "A;\x1bj" {
		t.Fatalf("register a holds %q", got)
	}
	expectText(t, d, "one;\ntwo\nthree\nfour\nfive\n")
	hs.vim("@a")
	expectText(t, d, "one;\ntwo;\nthree\nfour\nfive\n")
	hs.vim("@@")
	expectText(t, d, "one;\ntwo;\nthree;\nfour\nfive\n")
	// A count plays it as many times, and it stops where a motion fails,
	// at the last line, rather than going on.
	hs.vim("9@a")
	expectText(t, d, "one;\ntwo;\nthree;\nfour;\nfive;\n")
	// Each change it made undoes on its own, as in vim.
	hs.vim("u")
	expectText(t, d, "one;\ntwo;\nthree;\nfour;\nfive\n")
}

func TestVimMacroSearches(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "a x b x c x\n")
	hs.vim("qq")
	hs.vimPlay(d, "/x<CR>rYq")
	expectText(t, d, "a Y b x c x\n")
	if got := hs.a.vimRegs['q'].text; got != "/x\rrY" {
		t.Fatalf("register q holds %q", got)
	}
	hs.vim("2@q")
	expectText(t, d, "a Y b Y c Y\n")
}

func TestVimNormalAndGlobal(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "a\nb\nc\n")
	hs.vimPlay(d, ":%norm Ax<CR>")
	expectText(t, d, "ax\nbx\ncx\n")
	// The lines changed undo at once.
	hs.vim("u")
	expectText(t, d, "a\nb\nc\n")
	hs.vimPlay(d, ":g/b/norm ddp<CR>")
	expectText(t, d, "a\nc\nb\n")
	hs.vimPlay(d, ":g/./m0<CR>")
	expectText(t, d, "b\nc\na\n")
}

func TestVimJumps(t *testing.T) {
	text := strings.Repeat("line\n", 50)
	hs, d := vimHarness(t, "a.txt", text)
	hs.vim("10G")
	hs.vim("G")
	expectCursor(t, d, 49*5)
	hs.vim("<C-o>")
	expectCursor(t, d, 9*5)
	hs.vim("<C-o>")
	expectCursor(t, d, 0)
	hs.vim("<Tab>")
	expectCursor(t, d, 9*5)
	hs.vim("<Tab>")
	expectCursor(t, d, 49*5)
	// '' goes back to where the last jump was from, and again.
	hs.vim("gg''")
	expectCursor(t, d, 49*5)
	hs.vim("''")
	expectCursor(t, d, 0)
}

func TestVimMarksFollowEdits(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "one\ntwo\nthree\nfour\n")
	hs.vim("3Glma")
	hs.vim("ggdd")
	expectText(t, d, "two\nthree\nfour\n")
	hs.vim("G`a")
	expectCursor(t, d, 5)
	hs.vim("ggO")
	hs.insert("new")
	hs.vim("<Esc>")
	hs.vim("G'a")
	expectCursor(t, d, 8)
	hs.vimPlay(d, ":marks<CR>")
	if !strings.Contains(hs.a.vimMsg.text, "a 3:2") {
		t.Fatalf(":marks says %q", hs.a.vimMsg.text)
	}
}

func TestVimRegistersSay(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "alpha beta\n")
	hs.vim(`"ayiww"byiw`)
	hs.vimPlay(d, ":reg ab<CR>")
	if m := hs.a.vimMsg.text; !strings.Contains(m, `"a alpha`) || !strings.Contains(m, `"b beta`) {
		t.Fatalf(":reg says %q", m)
	}
	// Named registers leave the clipboard be.
	if got := hs.tt.Clipboard(); got != "" {
		t.Fatalf("the clipboard holds %q", got)
	}
}

func TestVimInsertDeletesBack(t *testing.T) {
	if ui.Cmd == ui.Ctrl {
		t.Skip("⌃W and ⌃U are the platform's shortcuts here")
	}
	hs, d := vimHarness(t, "a.txt", "    foo\n")
	hs.vim("A")
	hs.insert(" bar baz")
	hs.tt.Key(ui.Ctrl, ui.KeyW)
	hs.frames()
	expectText(t, d, "    foo bar \n")
	hs.tt.Key(ui.Ctrl, ui.KeyU)
	hs.frames()
	expectText(t, d, "    \n")
	hs.vim("<Esc>")
}

func TestVimCountedInsertRepeats(t *testing.T) {
	hs, d := vimHarness(t, "a.txt", "x\n")
	hs.vim("3a")
	hs.insert("ab")
	hs.vim("<Esc>")
	expectText(t, d, "xababab\n")
	expectCursor(t, d, 6)
	hs.vim("u")
	expectText(t, d, "x\n")
}
