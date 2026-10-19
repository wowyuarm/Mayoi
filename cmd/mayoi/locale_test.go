package main

import (
	"slices"
	"strings"
	"testing"

	"mayoi/internal/settings"
	"mayoi/locales"
)

// Every language labels every command, the same commands as English.
func TestCommandsInEveryLanguage(t *testing.T) {
	a := newApp(&fakeHost{}, settings.Default(), settings.Default().NewWindow())
	t.Cleanup(a.shutdown)
	var ids []string
	for _, cmd := range a.commandsIn(locales.In("en")) {
		ids = append(ids, cmd.id)
	}
	for _, lang := range []string{"zh-Hans", "ja"} {
		var got []string
		for _, cmd := range a.commandsIn(locales.In(lang)) {
			if cmd.label == "" {
				t.Errorf("%s: %s has no label", lang, cmd.id)
			}
			got = append(got, cmd.id)
		}
		if !slices.Equal(got, ids) {
			t.Errorf("%s: commands %v, want %v", lang, got, ids)
		}
	}
	if zh := locales.In("zh-Hans").CmdFileSave(); zh == locales.In("en").CmdFileSave() {
		t.Errorf("Save in Chinese is %q, as in English", zh)
	}
	if ja := locales.In("ja").CmdFileSave(); ja == locales.In("en").CmdFileSave() {
		t.Errorf("Save in Japanese is %q, as in English", ja)
	}
}

// The language commands come after the system's, one per language of the
// catalogs.
func TestLocaleCommands(t *testing.T) {
	a := newApp(&fakeHost{}, settings.Default(), settings.Default().NewWindow())
	t.Cleanup(a.shutdown)
	var got []string
	for _, cmd := range a.commands() {
		if tag, ok := strings.CutPrefix(cmd.id, localeCommand); ok {
			got = append(got, tag)
		}
	}
	want := []string{"system", "en", "ja", "zh-Hans"}
	if !slices.Equal(got, want) {
		t.Errorf("language commands %v, want %v", got, want)
	}
}

// The settings choose a language by any tag of it, as "zh" for
// "zh-Hans", and the system's when they name none.
func TestLocaleOn(t *testing.T) {
	a := newApp(&fakeHost{}, settings.Default(), settings.Default().NewWindow())
	t.Cleanup(a.shutdown)
	for _, tc := range []struct {
		setting, tag string
		on           bool
	}{
		{"", "system", true},
		{"", "en", false},
		{"zh", "zh-Hans", true},
		{"zh-Hans", "zh-Hans", true},
		{"zh-Hans", "system", false},
		{"ja-JP", "ja", true},
		{"ja", "en", false},
	} {
		a.settings.Locale = tc.setting
		if got := a.localeOn(tc.tag); got != tc.on {
			t.Errorf("Locale %q: localeOn(%q) = %v, want %v", tc.setting, tc.tag, got, tc.on)
		}
	}
}

// Choosing a language keeps it in the settings and tells the host.
func TestSetLocale(t *testing.T) {
	h := &fakeHost{}
	a := newApp(h, settings.Default(), settings.Default().NewWindow())
	t.Cleanup(a.shutdown)
	a.run(localeCommand + "ja")
	if a.settings.Locale != "ja" || h.locale != "ja" {
		t.Errorf("after choosing Japanese: settings %q, host %q", a.settings.Locale, h.locale)
	}
	a.run(localeCommand + "system")
	if a.settings.Locale != "" || h.locale != "" {
		t.Errorf("after choosing the system's: settings %q, host %q", a.settings.Locale, h.locale)
	}
}

// The palette finds a command by its English name in another language,
// after those its own name matches.
func TestPaletteMatchesEnglish(t *testing.T) {
	entries := []paletteEntry{
		{id: "file.save", title: "文件: 保存", en: "File: Save"},
		{id: "file.saveAll", title: "文件: 全部保存", en: "File: Save All"},
		{id: "edit.find", title: "编辑: 查找", en: "Edit: Find"},
	}
	var got []string
	for _, m := range paletteMatches(entries, "save") {
		got = append(got, entries[m.Index].id)
		if m.Positions != nil {
			t.Errorf("%s: positions %v of the English title", entries[m.Index].id, m.Positions)
		}
	}
	if !slices.Contains(got, "file.save") || !slices.Contains(got, "file.saveAll") || slices.Contains(got, "edit.find") {
		t.Errorf("save matches %v", got)
	}
	if m := paletteMatches(entries, "保存"); len(m) != 2 || m[0].Positions == nil {
		t.Errorf("保存 matches %v", m)
	}
}
