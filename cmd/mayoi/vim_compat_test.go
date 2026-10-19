package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// vimCase is a command typed in a text, and the text and cursor vim
// leaves, as testdata/vim.json has them from vim itself.
type vimCase struct {
	Text   string `json:"text"`
	Keys   string `json:"keys"`
	Want   string `json:"want"`
	Cursor int    `json:"cursor"`
}

// vimPlay types keys as hs.vim does, but types the text of insert mode.
func (hs *harness) vimPlay(d *editor, keys string) {
	hs.t.Helper()
	for len(keys) > 0 {
		if hs.a.vimLine.d != nil {
			// The command line takes the text, to Enter or Esc.
			end := strings.IndexAny(keys, "<")
			switch {
			case strings.HasPrefix(keys, "<CR>"):
				hs.tt.Key(0, ui.KeyEnter)
				keys = keys[4:]
			case strings.HasPrefix(keys, "<Esc>"):
				hs.tt.Key(0, ui.KeyEscape)
				keys = keys[5:]
			case end == 0:
				hs.tt.Type("<")
				keys = keys[1:]
			case end < 0:
				hs.tt.Type(keys)
				keys = ""
			default:
				hs.tt.Type(keys[:end])
				keys = keys[end:]
			}
			hs.frames()
			continue
		}
		if d.vim.mode != vimInsert {
			n := 1
			if keys[0] == '<' {
				if end := strings.IndexByte(keys, '>'); end > 2 && vimKeyName(keys[1:end]) {
					n = end + 1
				}
			}
			hs.vim(keys[:n])
			keys = keys[n:]
			continue
		}
		switch {
		case strings.HasPrefix(keys, "<Esc>"):
			hs.vim("<Esc>")
			keys = keys[5:]
		case strings.HasPrefix(keys, "<CR>"):
			hs.tt.Key(0, ui.KeyEnter)
			hs.frames()
			keys = keys[4:]
		case strings.HasPrefix(keys, "<BS>"):
			hs.tt.Key(0, ui.KeyBackspace)
			hs.frames()
			keys = keys[4:]
		default:
			hs.insert(keys[:1])
			keys = keys[1:]
		}
	}
}

// TestVimLikeVim types the commands of testdata/vim.json and checks that
// they leave the text and the cursor as vim does.
func TestVimLikeVim(t *testing.T) {
	data, err := os.ReadFile("testdata/vim.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []vimCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	// One editor for them all, its text, history, register and state put
	// back for each.
	hs, d := vimHarness(t, "a.txt", "")
	for _, c := range cases {
		t.Run(c.Keys, func(t *testing.T) {
			hs.t = t
			// Unseen, so that the text put back is not typing, which
			// would close its brackets.
			d.hist = history{}
			d.buf.Replace(0, d.buf.Len(), c.Text)
			d.observe()
			d.indent = detectIndent(d.buf.Snapshot(), d.path)
			d.vim, d.marks = vimState{}, nil
			hs.a.vimReg, hs.a.vimRegs = vimRegister{}, nil
			hs.a.vimSearch, hs.a.vimLastSub, hs.a.vimLastEx = vimSearch{}, "", ""
			hs.tt.SetClipboard("")
			d.selectRange(0, 0, false)
			hs.frames()
			hs.vimPlay(d, c.Keys)
			if d.vim.mode != vimNormal {
				hs.vim("<Esc>")
			}
			if got := d.buf.String(); got != c.Want {
				t.Errorf("%q in %q:\ntext %q\nwant %q", c.Keys, c.Text, got, c.Want)
				return
			}
			if d.sel != [2]int{c.Cursor, c.Cursor} {
				t.Errorf("%q in %q: cursor %v, want %d", c.Keys, c.Text, d.sel, c.Cursor)
			}
		})
	}
}
