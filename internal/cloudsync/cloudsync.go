// Package cloudsync syncs Mayoi's preferences and keyboard shortcuts with
// the person's Mon account, as one settings document of Mon's:
//
//	{"settings": {"fontSize": 14, …}, "keybindings": "// keybindings.json as it is\n[…]"}
//
// The preferences merge key by key against the snapshot of the last sync,
// the base: a key changed on one side only takes that side's value, and
// one changed on both the side that syncs now. keybindings.json is text,
// which keeps its comments: changed on both sides, this machine's stays
// and the account's comes back in Result.KeybindingsConflict.
package cloudsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"mayoi/internal/mon"
)

// App is Mayoi's client id with Mon, and its settings document's name.
const App = "mayoi"

// Snapshot is what syncs: the preferences by their keys in settings.json,
// and keybindings.json's text, "" for no file.
type Snapshot struct {
	Settings    map[string]json.RawMessage `json:"settings,omitempty"`
	Keybindings string                     `json:"keybindings,omitempty"`
}

// Equal reports whether s and o hold the same preferences and shortcuts.
func (s Snapshot) Equal(o Snapshot) bool {
	return s.Keybindings == o.Keybindings && maps.EqualFunc(s.Settings, o.Settings, sameJSON)
}

func (s Snapshot) clone() Snapshot {
	s.Settings = maps.Clone(s.Settings)
	return s
}

func sameJSON(a, b json.RawMessage) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

// Merge merges what changed here since base, local, with what changed in
// the account, remote. Where both changed a preference, local's stays.
// Where both changed keybindings.json differently, local's stays and
// conflict is the account's.
func Merge(base, local, remote Snapshot) (merged Snapshot, conflict string) {
	merged.Settings = map[string]json.RawMessage{}
	keys := map[string]bool{}
	for _, m := range []map[string]json.RawMessage{base.Settings, local.Settings, remote.Settings} {
		for k := range m {
			keys[k] = true
		}
	}
	for k := range keys {
		side := remote.Settings
		if changed(base.Settings, local.Settings, k) {
			side = local.Settings
		}
		if v, ok := side[k]; ok {
			merged.Settings[k] = v
		}
	}
	merged.Keybindings = remote.Keybindings
	if local.Keybindings != base.Keybindings {
		merged.Keybindings = local.Keybindings
		if remote.Keybindings != base.Keybindings && remote.Keybindings != local.Keybindings {
			conflict = remote.Keybindings
		}
	}
	return merged, conflict
}

// changed reports whether the key of to differs from that of from, as
// being set in one only.
func changed(from, to map[string]json.RawMessage, k string) bool {
	a, aok := from[k]
	b, bok := to[k]
	return aok != bok || aok && !sameJSON(a, b)
}

// Local is this machine's preferences and shortcuts. The methods are
// called on the engine's goroutine.
type Local interface {
	// Read returns the preferences and shortcuts as they are now.
	Read() (Snapshot, error)
	// Apply makes s the preferences and shortcuts, and conflict, unless
	// "", the account's keybindings.json that s leaves out. Where they
	// are no longer read, as Read returned them, it changes nothing and
	// fails with ErrStale.
	Apply(read, s Snapshot, conflict string) error
	// IsDefault reports whether s is what a first launch has, which a
	// first sync replaces without asking.
	IsDefault(s Snapshot) bool
	// Known reports whether this version of Mayoi has the preference
	// key: one it does not have, of a newer Mayoi's, it keeps as the
	// account has it.
	Known(key string) bool
	// Kept returns what stays on this machine, as the person chose: the
	// keys of the preferences, and keybindings set for keybindings.json.
	// The account keeps its own of them, and Apply leaves this machine's.
	Kept() (keys []string, keybindings bool)
}

// State is what the engine keeps between launches, in sync.json: the
// account, and the version of the account's document and the snapshot of
// the last sync. Base is nil until the first sync.
type State struct {
	UserID string `json:"userId"`
	Name   string `json:"name,omitempty"`
	Email  string `json:"email,omitempty"`
	// Avatar is the URL of the person's picture.
	Avatar  string    `json:"avatar,omitempty"`
	Version int64     `json:"version"`
	Base    *Snapshot `json:"base,omitempty"`
	// SyncedAt is when this machine and the account last agreed.
	SyncedAt time.Time `json:"syncedAt,omitzero"`
}

// Choice is how a first sync goes, where both this machine and the
// account have preferences of their own.
type Choice int

const (
	// Ask fails a first sync with ErrNeedsChoice when both sides have
	// preferences of their own.
	Ask Choice = iota
	// UseAccount replaces this machine's with the account's.
	UseAccount
	// UseLocal replaces the account's with this machine's.
	UseLocal
)

// ErrNeedsChoice is what a first sync fails with when this machine and
// the account both have preferences of their own: sync again with
// UseAccount or UseLocal.
var ErrNeedsChoice = errors.New("cloudsync: this machine and the account both have settings")

// ErrStale is what Apply fails with when this machine's settings changed
// since they were read, as the person changed one as a sync ran. A sync
// reads them again and merges anew, staleTries times at most, and fails
// with it then: the change syncs as it is pushed.
var ErrStale = errors.New("cloudsync: this machine's settings changed as they synced")

const staleTries = 3

// Result says what a sync did.
type Result struct {
	// Applied is set when the account's changes applied here, Pushed when
	// this machine's were saved to the account.
	Applied, Pushed bool
	// KeybindingsConflict is the account's keybindings.json, which
	// changed here too, and which Apply was given to keep beside it.
	KeybindingsConflict string
}

// Engine syncs Local with the account. Its methods may be called from any
// goroutine; one sync runs at a time.
type Engine struct {
	Client *mon.Client
	Local  Local
	path   string

	mu    sync.Mutex // held through a sync
	smu   sync.Mutex // guards state
	state State
}

// New returns an engine whose state is kept at path, as it last was.
func New(client *mon.Client, local Local, path string) *Engine {
	e := &Engine{Client: client, Local: local, path: path}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &e.state)
	}
	return e
}

// State returns the engine's state.
func (e *Engine) State() State {
	e.smu.Lock()
	defer e.smu.Unlock()
	return e.state
}

// SignedIn starts syncing for the person u, whose token the client has,
// with a first sync to come.
func (e *Engine) SignedIn(u *mon.User) error {
	return e.setState(State{UserID: u.ID, Name: u.Name, Email: u.Email, Avatar: u.AvatarURL})
}

// Refreshed takes the person's name, email and picture as the account
// has them now, keeping the rest.
func (e *Engine) Refreshed(u *mon.User) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.State()
	if st.UserID != u.ID || st.Name == u.Name && st.Email == u.Email && st.Avatar == u.AvatarURL {
		return nil
	}
	st.Name, st.Email, st.Avatar = u.Name, u.Email, u.AvatarURL
	return e.setState(st)
}

// Forget forgets the account and the last sync, as the person signs out.
func (e *Engine) Forget() {
	e.smu.Lock()
	e.state = State{}
	e.smu.Unlock()
	os.Remove(e.path)
}

func (e *Engine) setState(st State) error {
	e.smu.Lock()
	e.state = st
	e.smu.Unlock()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(e.path), 0o755)
	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, e.path)
}

// Push saves this machine's changes to the account, unless there are none
// since the last sync: then it does not ask the account at all.
func (e *Engine) Push(ctx context.Context) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.State()
	if st.Base == nil {
		return Result{}, nil
	}
	local, err := e.Local.Read()
	if err != nil {
		return Result{}, err
	}
	if e.withKept(e.withUnknown(*st.Base, local), *st.Base).Equal(*st.Base) {
		return Result{}, nil
	}
	return e.sync(ctx, Ask)
}

// Sync fetches the account's settings, merges them with this machine's
// and saves the merge on both sides. A first sync goes as choice says.
// mon.ErrUnauthorized means the person signed out elsewhere, as from the
// account page: the engine has forgotten the account.
func (e *Engine) Sync(ctx context.Context, choice Choice) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sync(ctx, choice)
}

func (e *Engine) sync(ctx context.Context, choice Choice) (Result, error) {
	st := e.State()
	if st.UserID == "" {
		return Result{}, mon.ErrUnauthorized
	}
	remote, version, err := e.fetch(ctx)
	if err != nil {
		return Result{}, err
	}
	local, err := e.Local.Read()
	if err != nil {
		return Result{}, err
	}
	var base Snapshot
	if st.Base != nil {
		base = *st.Base
	} else {
		// The first sync: the side that has nothing of its own takes the
		// other's, else the one chosen wins.
		switch {
		case version == 0 || choice == UseLocal:
			base = remote
		case e.Local.IsDefault(local) || choice == UseAccount:
			base = local
		default:
			return Result{}, ErrNeedsChoice
		}
	}
	// This machine's changes are those of local since base, which merge
	// with each version of the account's this sync gets. What stays on
	// this machine is the account's, as though unchanged here.
	mine := e.withUnknown(base, local)
	var res Result
	var applied *Snapshot
	stale := 0
	for {
		ours := e.withKept(mine, remote)
		merged, conflict := Merge(base, ours, remote)
		now := ours
		if applied != nil {
			now = *applied
		}
		if !merged.Equal(now) || conflict != "" && conflict != res.KeybindingsConflict {
			err := e.Local.Apply(local, merged.clone(), conflict)
			if errors.Is(err, ErrStale) && stale < staleTries {
				// The person changed a setting since it was read: merge
				// that change too, rather than undo it.
				stale++
				fresh, err := e.Local.Read()
				if err != nil {
					return res, err
				}
				mine, local = since(mine, local, fresh), fresh
				continue
			}
			if err != nil {
				return res, err
			}
			res.Applied = true
			res.KeybindingsConflict = conflict
			applied = &merged
			// As applied, which a later Apply of this sync checks against.
			if local, err = e.Local.Read(); err != nil {
				return res, err
			}
		}
		if merged.Equal(remote) && version > 0 {
			st.Version, st.Base, st.SyncedAt = version, &merged, time.Now()
			return res, e.setState(st)
		}
		saved, err := e.Client.PutSettings(ctx, App, version, merged)
		var conflictErr *mon.ConflictError
		if errors.As(err, &conflictErr) {
			// Another machine saved since: merge this machine's changes
			// again, with what it saved.
			if remote, err = decode(conflictErr.Current.Data); err != nil {
				return res, err
			}
			version = conflictErr.Current.Version
			continue
		}
		if err != nil {
			return res, e.signedOut(err)
		}
		res.Pushed = true
		st.Version, st.Base, st.SyncedAt = saved.Version, &merged, time.Now()
		return res, e.setState(st)
	}
}

// since returns mine with what changed from was to now, read as a sync
// ran: the person's changes of then, over this machine's of before.
func since(mine, was, now Snapshot) Snapshot {
	out := mine.clone()
	if out.Settings == nil {
		out.Settings = map[string]json.RawMessage{}
	}
	for _, m := range []map[string]json.RawMessage{was.Settings, now.Settings} {
		for k := range m {
			if !changed(was.Settings, now.Settings, k) {
				continue
			}
			if v, ok := now.Settings[k]; ok {
				out.Settings[k] = v
			} else {
				delete(out.Settings, k)
			}
		}
	}
	if now.Keybindings != was.Keybindings {
		out.Keybindings = now.Keybindings
	}
	return out
}

// withUnknown returns local with the preferences of base this version of
// Mayoi does not know, as unchanged, rather than as removed.
func (e *Engine) withUnknown(base, local Snapshot) Snapshot {
	out := local
	out.Settings = maps.Clone(local.Settings)
	if out.Settings == nil {
		out.Settings = map[string]json.RawMessage{}
	}
	for k, v := range base.Settings {
		if _, ok := out.Settings[k]; !ok && !e.Local.Known(k) {
			out.Settings[k] = v
		}
	}
	return out
}

// withKept returns local with what stays on this machine as of has it,
// so that it neither changes the account's nor takes it.
func (e *Engine) withKept(local, of Snapshot) Snapshot {
	keys, keybindings := e.Local.Kept()
	if len(keys) == 0 && !keybindings {
		return local
	}
	out := local
	out.Settings = maps.Clone(local.Settings)
	if out.Settings == nil {
		out.Settings = map[string]json.RawMessage{}
	}
	for k := range out.Settings {
		if slices.Contains(keys, k) {
			delete(out.Settings, k)
		}
	}
	for _, k := range keys {
		if v, ok := of.Settings[k]; ok {
			out.Settings[k] = v
		}
	}
	if keybindings {
		out.Keybindings = of.Keybindings
	}
	return out
}

// fetch returns the account's settings and their version, 0 for none.
func (e *Engine) fetch(ctx context.Context) (Snapshot, int64, error) {
	st, err := e.Client.GetSettings(ctx, App)
	if err != nil {
		return Snapshot{}, 0, e.signedOut(err)
	}
	s, err := decode(st.Data)
	return s, st.Version, err
}

func decode(data json.RawMessage) (Snapshot, error) {
	var s Snapshot
	if len(data) == 0 || string(data) == "null" {
		return s, nil
	}
	err := json.Unmarshal(data, &s)
	return s, err
}

// signedOut forgets the account when err says its token is no more.
func (e *Engine) signedOut(err error) error {
	if errors.Is(err, mon.ErrUnauthorized) {
		e.Forget()
	}
	return err
}
