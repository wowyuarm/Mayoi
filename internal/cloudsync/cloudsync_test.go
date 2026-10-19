package cloudsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mayoi/internal/mon"
)

// fakeMon is Mon's settings API for one person, as Mon answers it.
type fakeMon struct {
	mu      sync.Mutex
	version int64
	data    json.RawMessage
	revoked bool
	puts    int
	// beforePut runs before a PUT is answered, as another machine saving.
	beforePut func()
}

func (f *fakeMon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	if f.revoked {
		f.mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method == "PUT" && f.beforePut != nil {
		hook := f.beforePut
		f.beforePut = nil
		f.mu.Unlock()
		hook()
		f.mu.Lock()
	}
	defer f.mu.Unlock()
	doc := func() map[string]any {
		return map[string]any{"app": App, "version": f.version, "data": f.data}
	}
	switch r.Method {
	case "GET":
		json.NewEncoder(w).Encode(doc())
	case "PUT":
		var body struct {
			BaseVersion int64           `json:"base_version"`
			Data        json.RawMessage `json:"data"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.BaseVersion != f.version {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{"error": "version_conflict", "current": doc()})
			return
		}
		f.puts++
		f.version++
		f.data = body.Data
		json.NewEncoder(w).Encode(doc())
	}
}

// save saves s as another machine would.
func (f *fakeMon) save(s Snapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data, _ = json.Marshal(s)
	f.version++
}

func (f *fakeMon) snapshot(t *testing.T) Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := decode(f.data)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// fakeLocal is a machine's preferences and shortcuts.
type fakeLocal struct {
	s        Snapshot
	conflict string
	applies  int
	// keep and keepKeys are what stays on the machine.
	keep     []string
	keepKeys bool
	// beforeApply runs before an Apply, as the person changing a setting
	// as a sync runs.
	beforeApply func()
}

func (l *fakeLocal) Read() (Snapshot, error) {
	out := l.s
	out.Settings = maps.Clone(l.s.Settings)
	return out, nil
}

func (l *fakeLocal) Apply(read, s Snapshot, conflict string) error {
	if hook := l.beforeApply; hook != nil {
		l.beforeApply = nil
		hook()
	}
	if !l.s.Equal(read) {
		return ErrStale
	}
	for _, k := range l.keep {
		delete(s.Settings, k)
		if v, ok := l.s.Settings[k]; ok {
			s.Settings[k] = v
		}
	}
	if l.keepKeys {
		s.Keybindings = l.s.Keybindings
	}
	l.s, l.conflict = s, conflict
	l.applies++
	return nil
}

func (l *fakeLocal) IsDefault(s Snapshot) bool { return len(s.Settings) == 0 && s.Keybindings == "" }
func (l *fakeLocal) Known(key string) bool     { return key != "future" }
func (l *fakeLocal) Kept() ([]string, bool)    { return l.keep, l.keepKeys }

func settingsOf(kv ...string) map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = json.RawMessage(kv[i+1])
	}
	return m
}

func newEngine(t *testing.T, f *fakeMon, l *fakeLocal) *Engine {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := mon.New(srv.URL)
	c.Token = "mon_test"
	e := New(c, l, filepath.Join(t.TempDir(), "sync.json"))
	if err := e.SignedIn(&mon.User{ID: "u1", Name: "Ada"}); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestMerge(t *testing.T) {
	base := Snapshot{Settings: settingsOf("fontSize", "13", "theme", `"claude"`, "vim", "true"), Keybindings: "[]"}
	local := Snapshot{Settings: settingsOf("fontSize", "15", "theme", `"claude"`), Keybindings: "[]"}
	remote := Snapshot{Settings: settingsOf("fontSize", "14", "theme", `"qoder"`, "vim", "true", "blame", "false"), Keybindings: "[1]"}
	merged, conflict := Merge(base, local, remote)
	want := settingsOf("fontSize", "15", "theme", `"qoder"`, "blame", "false")
	if !maps.EqualFunc(merged.Settings, want, sameJSON) {
		t.Errorf("settings = %s, want %s", merged.Settings, want)
	}
	if merged.Keybindings != "[1]" || conflict != "" {
		t.Errorf("keybindings = %q, conflict %q", merged.Keybindings, conflict)
	}

	local.Keybindings = "[2]"
	merged, conflict = Merge(base, local, remote)
	if merged.Keybindings != "[2]" || conflict != "[1]" {
		t.Errorf("both changed: keybindings = %q, conflict %q", merged.Keybindings, conflict)
	}
	remote.Keybindings = "[2]"
	if _, conflict = Merge(base, local, remote); conflict != "" {
		t.Errorf("the same change is no conflict: %q", conflict)
	}
}

func TestFirstSyncUploadsToAnEmptyAccount(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: Snapshot{Settings: settingsOf("fontSize", "15"), Keybindings: "[]"}}
	e := newEngine(t, f, l)
	res, err := e.Sync(context.Background(), Ask)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pushed || res.Applied {
		t.Errorf("result = %+v", res)
	}
	if got := f.snapshot(t); !got.Equal(l.s) {
		t.Errorf("account = %+v, want %+v", got, l.s)
	}
	if st := e.State(); st.Version != 1 || st.Base == nil {
		t.Errorf("state = %+v", st)
	}
}

func TestFirstSyncOfADefaultMachineTakesTheAccounts(t *testing.T) {
	f := &fakeMon{}
	f.save(Snapshot{Settings: settingsOf("theme", `"claude"`), Keybindings: "[1]"})
	l := &fakeLocal{}
	e := newEngine(t, f, l)
	res, err := e.Sync(context.Background(), Ask)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || res.Pushed {
		t.Errorf("result = %+v", res)
	}
	if !l.s.Equal(f.snapshot(t)) {
		t.Errorf("local = %+v", l.s)
	}
}

func TestFirstSyncAsksWhenBothHaveSettings(t *testing.T) {
	account := Snapshot{Settings: settingsOf("theme", `"claude"`, "vim", "true")}
	mine := Snapshot{Settings: settingsOf("fontSize", "15"), Keybindings: "[]"}
	for _, tc := range []struct {
		choice Choice
		want   Snapshot
	}{{UseAccount, account}, {UseLocal, mine}} {
		f := &fakeMon{}
		f.save(account)
		l := &fakeLocal{s: mine}
		e := newEngine(t, f, l)
		if _, err := e.Sync(context.Background(), Ask); !errors.Is(err, ErrNeedsChoice) {
			t.Fatalf("err = %v, want ErrNeedsChoice", err)
		}
		if e.State().Base != nil || l.applies != 0 || f.puts != 0 {
			t.Fatal("a sync waiting for a choice changed something")
		}
		if _, err := e.Sync(context.Background(), tc.choice); err != nil {
			t.Fatal(err)
		}
		if !l.s.Equal(tc.want) || !f.snapshot(t).Equal(tc.want) {
			t.Errorf("choice %d: local %+v, account %+v, want %+v", tc.choice, l.s, f.snapshot(t), tc.want)
		}
	}
}

func TestSyncMergesBothSides(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: Snapshot{Settings: settingsOf("fontSize", "13", "theme", `"claude"`)}}
	e := newEngine(t, f, l)
	ctx := context.Background()
	if _, err := e.Sync(ctx, Ask); err != nil {
		t.Fatal(err)
	}
	// Another machine changes the theme, this one the font size.
	f.save(Snapshot{Settings: settingsOf("fontSize", "13", "theme", `"qoder"`)})
	l.s.Settings["fontSize"] = json.RawMessage("16")
	res, err := e.Push(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := settingsOf("fontSize", "16", "theme", `"qoder"`)
	if !res.Applied || !res.Pushed || !maps.EqualFunc(l.s.Settings, want, sameJSON) || !f.snapshot(t).Equal(l.s) {
		t.Errorf("result %+v, local %s, account %+v", res, l.s.Settings, f.snapshot(t))
	}
}

func TestPushWithoutChangesAsksNothing(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: Snapshot{Settings: settingsOf("fontSize", "13")}}
	e := newEngine(t, f, l)
	ctx := context.Background()
	if _, err := e.Sync(ctx, Ask); err != nil {
		t.Fatal(err)
	}
	f.revoked = true // any request fails
	if _, err := e.Push(ctx); err != nil {
		t.Errorf("push without changes: %v", err)
	}
}

func TestSyncRetriesAConflict(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: Snapshot{Settings: settingsOf("fontSize", "13", "theme", `"claude"`)}}
	e := newEngine(t, f, l)
	ctx := context.Background()
	if _, err := e.Sync(ctx, Ask); err != nil {
		t.Fatal(err)
	}
	l.s.Settings["fontSize"] = json.RawMessage("16")
	// Another machine saves between this one's GET and its PUT.
	f.beforePut = func() { f.save(Snapshot{Settings: settingsOf("fontSize", "13", "theme", `"qoder"`, "vim", "true")}) }
	if _, err := e.Push(ctx); err != nil {
		t.Fatal(err)
	}
	want := settingsOf("fontSize", "16", "theme", `"qoder"`, "vim", "true")
	if !maps.EqualFunc(l.s.Settings, want, sameJSON) || !f.snapshot(t).Equal(l.s) {
		t.Errorf("local %s, account %+v", l.s.Settings, f.snapshot(t))
	}
	if st := e.State(); st.Version != f.version {
		t.Errorf("version = %d, want %d", st.Version, f.version)
	}
}

func TestAChangeAsItSyncsStays(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: Snapshot{Settings: settingsOf("fontSize", "13", "theme", `"claude"`), Keybindings: "[0]"}}
	e := newEngine(t, f, l)
	ctx := context.Background()
	if _, err := e.Sync(ctx, Ask); err != nil {
		t.Fatal(err)
	}
	f.save(Snapshot{Settings: settingsOf("fontSize", "13", "theme", `"qoder"`), Keybindings: "[0]"})
	// The person changes the font size and the shortcuts after the sync
	// read them, before it applies the account's theme.
	l.beforeApply = func() {
		l.s.Settings["fontSize"] = json.RawMessage("16")
		l.s.Keybindings = "[1]"
	}
	res, err := e.Sync(ctx, Ask)
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{Settings: settingsOf("fontSize", "16", "theme", `"qoder"`), Keybindings: "[1]"}
	if !res.Applied || !res.Pushed || !l.s.Equal(want) || !f.snapshot(t).Equal(want) || !e.State().Base.Equal(want) {
		t.Errorf("result %+v, local %+v, account %+v", res, l.s, f.snapshot(t))
	}
}

func TestSyncGivesUpOnSettingsThatKeepChanging(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: Snapshot{Settings: settingsOf("fontSize", "13")}}
	e := newEngine(t, f, l)
	ctx := context.Background()
	if _, err := e.Sync(ctx, Ask); err != nil {
		t.Fatal(err)
	}
	f.save(Snapshot{Settings: settingsOf("fontSize", "13", "vim", "true")})
	size := 13
	var change func()
	change = func() {
		size++
		l.s.Settings["fontSize"] = json.RawMessage(fmt.Sprint(size))
		l.beforeApply = change
	}
	l.beforeApply = change
	puts := f.puts
	if _, err := e.Sync(ctx, Ask); !errors.Is(err, ErrStale) {
		t.Fatalf("err = %v, want ErrStale", err)
	}
	if l.applies != 0 || f.puts != puts {
		t.Errorf("applies %d, puts %d: a stale sync changed something", l.applies, f.puts-puts)
	}
}

func TestKeybindingsConflictKeepsThisMachines(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: Snapshot{Keybindings: "[0]"}}
	e := newEngine(t, f, l)
	ctx := context.Background()
	if _, err := e.Sync(ctx, Ask); err != nil {
		t.Fatal(err)
	}
	f.save(Snapshot{Keybindings: "[remote]"})
	l.s.Keybindings = "[local]"
	res, err := e.Push(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.KeybindingsConflict != "[remote]" || l.conflict != "[remote]" || l.s.Keybindings != "[local]" || f.snapshot(t).Keybindings != "[local]" {
		t.Errorf("result %+v, local %+v, account %+v", res, l.s, f.snapshot(t))
	}
}

func TestUnknownPreferencesStay(t *testing.T) {
	f := &fakeMon{}
	f.save(Snapshot{Settings: settingsOf("fontSize", "13", "future", "1")})
	l := &fakeLocal{}
	e := newEngine(t, f, l)
	ctx := context.Background()
	if _, err := e.Sync(ctx, Ask); err != nil {
		t.Fatal(err)
	}
	// This Mayoi drops the key it does not know, as settings.json does.
	delete(l.s.Settings, "future")
	l.s.Settings["fontSize"] = json.RawMessage("14")
	if _, err := e.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.snapshot(t).Settings; !sameJSON(got["future"], json.RawMessage("1")) || !sameJSON(got["fontSize"], json.RawMessage("14")) {
		t.Errorf("account = %s", got)
	}
}

func TestRevokedTokenForgetsTheAccount(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{}
	e := newEngine(t, f, l)
	f.revoked = true
	if _, err := e.Sync(context.Background(), Ask); !errors.Is(err, mon.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
	if e.State().UserID != "" {
		t.Error("the account stayed")
	}
	again := New(e.Client, l, e.path)
	if again.State().UserID != "" {
		t.Error("sync.json kept the account")
	}
}

func TestStateLastsBetweenLaunches(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: Snapshot{Settings: settingsOf("fontSize", "15")}}
	e := newEngine(t, f, l)
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	again := New(e.Client, l, e.path)
	if st := again.State(); st.UserID != "u1" || st.Name != "Ada" || st.Version != 1 || st.Base == nil || !st.Base.Equal(l.s) {
		t.Errorf("state = %+v", st)
	}
}

func TestKeptPreferencesStayOnEachMachine(t *testing.T) {
	f := &fakeMon{}
	f.save(Snapshot{Settings: settingsOf("fontSize", "13", "theme", `"claude"`), Keybindings: "[account]"})
	l := &fakeLocal{s: Snapshot{Settings: settingsOf("fontSize", "18", "theme", `"claude"`), Keybindings: "[mine]"}, keep: []string{"fontSize"}, keepKeys: true}
	e := newEngine(t, f, l)
	ctx := context.Background()
	res, err := e.Sync(ctx, UseAccount)
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || res.Pushed {
		t.Errorf("first sync: %+v", res)
	}
	// Changing what stays here neither saves to the account nor asks.
	l.s.Settings["fontSize"] = json.RawMessage("20")
	l.s.Keybindings = "[mine, changed]"
	f.revoked = true
	if _, err := e.Push(ctx); err != nil {
		t.Fatalf("push of what stays here: %v", err)
	}
	f.revoked = false
	// Another machine's change of them does not come here either.
	f.save(Snapshot{Settings: settingsOf("fontSize", "11", "theme", `"qoder"`), Keybindings: "[other]"})
	if res, err = e.Sync(ctx, Ask); err != nil {
		t.Fatal(err)
	}
	if got := l.s.Settings; !sameJSON(got["fontSize"], json.RawMessage("20")) || !sameJSON(got["theme"], json.RawMessage(`"qoder"`)) || l.s.Keybindings != "[mine, changed]" {
		t.Errorf("local = %s, %q", got, l.s.Keybindings)
	}
	if got := f.snapshot(t); !sameJSON(got.Settings["fontSize"], json.RawMessage("11")) || got.Keybindings != "[other]" {
		t.Errorf("account = %s, %q", got.Settings, got.Keybindings)
	}
	if res.Pushed || res.KeybindingsConflict != "" {
		t.Errorf("result = %+v", res)
	}
	// Up to date, a sync applies nothing.
	if res, err = e.Sync(ctx, Ask); err != nil || res.Applied || res.Pushed {
		t.Errorf("sync again: %+v, %v", res, err)
	}
}

func TestSyncedAt(t *testing.T) {
	f := &fakeMon{}
	l := &fakeLocal{s: Snapshot{Settings: settingsOf("fontSize", "15")}}
	e := newEngine(t, f, l)
	if !e.State().SyncedAt.IsZero() {
		t.Fatal("synced before a sync")
	}
	before := time.Now()
	if _, err := e.Sync(context.Background(), Ask); err != nil {
		t.Fatal(err)
	}
	if at := e.State().SyncedAt; at.Before(before) {
		t.Errorf("synced at %v, before %v", at, before)
	}
	if at := New(e.Client, l, e.path).State().SyncedAt; at.IsZero() {
		t.Error("sync.json did not keep the time")
	}
}
