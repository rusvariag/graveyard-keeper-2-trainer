//go:build windows

// Modern trainer window: an embedded WebView2 (Edge) view inside the app's own window.
// No browser and no console are involved. If the WebView2 runtime is missing,
// main() falls back to the classic native window (gui_windows.go).
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
)

//go:embed webui.html
var webUIHTML string

// webState is what the page polls once a second.
type webState struct {
	mu        sync.Mutex
	Ready     bool   `json:"ready"`
	Text      string `json:"text"`
	Level     string `json:"level"` // ok | warn | err
	Warn      string `json:"warn"`
	instant   atomic.Bool
	connectMu sync.Mutex
	// wanted cheat state, re-sent after every (re)connect because a restarted game starts clean
	cheats map[string]bool
	speeds map[string]float64 // move, game, tech, friend, energy, stamina; 1 = normal
}

func (s *webState) cheatState() (map[string]bool, map[string]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := map[string]bool{}
	for k, v := range s.cheats {
		c[k] = v
	}
	sp := map[string]float64{}
	for k, v := range s.speeds {
		sp[k] = v
	}
	return c, sp
}

func (s *webState) reapplyCheats() {
	s.mu.Lock()
	on := []string{}
	for k, v := range s.cheats {
		if v {
			on = append(on, k)
		}
	}
	speeds := map[string]float64{}
	for k, v := range s.speeds {
		if v != 1 {
			speeds[k] = v
		}
	}
	s.mu.Unlock()
	for _, k := range on {
		cheatSet(k, true)
	}
	for k, v := range speeds {
		cheatSpeed(k, v)
	}
}

func (s *webState) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"ready": s.Ready, "text": s.Text, "level": s.Level, "warn": s.Warn}
}

func (s *webState) set(ready bool, text, level, warn string) {
	s.mu.Lock()
	wasReady := s.Ready
	s.Ready, s.Text, s.Level, s.Warn = ready, text, level, warn
	s.mu.Unlock()
	// a restarted game starts with instant craft off: re-send it after every (re)connect
	if ready && !wasReady {
		if s.instant.Load() {
			setInstantCraft(true)
		}
		s.reapplyCheats()
	}
}

// connect probes the game and injects the helper when needed (same rules as the classic window).
func (s *webState) connect() {
	if !s.connectMu.TryLock() {
		return
	}
	defer s.connectMu.Unlock()
	switch st := probeHelper(); {
	case st == helperReady:
		s.set(true, "Connected to Graveyard Keeper 2", "ok", "")
	case st == helperOutdated:
		s.set(false, "Old helper in the game - restart the game once", "warn", outdatedMsg)
	case !gameRunning():
		s.set(false, "Waiting for the game…", "err", "")
	default:
		if err := connectToGame(); err == nil {
			s.set(true, "Connected to Graveyard Keeper 2", "ok", "")
		} else {
			s.set(false, "Game found - connecting…", "warn", err.Error())
		}
	}
}

type apiResult struct {
	OK   bool   `json:"ok"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

func res(ok bool, msg string) apiResult { return apiResult{OK: ok, Msg: msg} }

// handleAPI runs one page request (off the UI thread).
func handleAPI(st *webState, items []Item, action string, raw string) apiResult {
	var a struct {
		ID     string  `json:"id"`
		Count  int     `json:"count"`
		R      int     `json:"r"`
		G      int     `json:"g"`
		B      int     `json:"b"`
		Copper int     `json:"copper"`
		On     bool    `json:"on"`
		Target string  `json:"target"`
		Hour   int     `json:"hour"`
		Minute int     `json:"minute"`
		Speed  int     `json:"speed"`
		Rested bool    `json:"rested"`
		Cmd    string  `json:"cmd"`
		Name   string  `json:"name"`
		Which  string  `json:"which"`
		X      float64 `json:"x"`
		Value  int     `json:"value"`
	}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			return res(false, "bad request: "+err.Error())
		}
	}
	switch action {
	case "init":
		cheats, speeds := st.cheatState()
		return apiResult{OK: true, Data: map[string]any{"version": appVersion, "items": items, "instant": st.instant.Load(), "cheats": cheats, "speeds": speeds}}
	case "status":
		out := st.snapshot()
		if st.Ready {
			z, zn := zombieOpen()
			c, cd := corpseOpen()
			out["zombie"], out["zombieName"], out["corpse"], out["corpseDesc"] = z, zn, c, cd
			if t, err := timeGet(); err == nil {
				out["time"] = map[string]any{"day": t.Day, "weekday": t.Weekday, "weekdayName": weekdayNames[clampWeekday(t.Weekday)],
					"tod": t.TimeOfDay, "clock": t.Clock(), "dayMinutes": t.DayMinutes, "speed": t.Speed, "ff": t.FFTarget}
			}
		}
		return apiResult{OK: true, Data: out}
	case "reconnect":
		st.connect()
		return apiResult{OK: true, Msg: st.snapshot()["text"].(string)}
	case "addItem":
		return res(addItem(a.ID, a.Count))
	case "tech":
		return res(addTech(a.R, a.G, a.B))
	case "money":
		return res(changeMoney(a.Copper))
	case "happiness":
		h, msg, err := happiness(a.Value)
		if err != nil {
			return res(false, msg)
		}
		return apiResult{OK: true, Msg: msg, Data: h}
	case "instant":
		st.instant.Store(a.On)
		if !st.Ready {
			return res(true, "Instant craft will be applied when the trainer connects to the game.")
		}
		return res(setInstantCraft(a.On))
	case "cheatSet":
		st.mu.Lock()
		st.cheats[a.Name] = a.On
		st.mu.Unlock()
		if !st.Ready {
			return res(true, "Will be applied when the trainer connects to the game.")
		}
		return res(cheatSet(a.Name, a.On))
	case "cheatSpeed":
		st.mu.Lock()
		st.speeds[a.Which] = a.X
		st.mu.Unlock()
		if !st.Ready {
			return res(true, "Will be applied when the trainer connects to the game.")
		}
		return res(cheatSpeed(a.Which, a.X))
	case "cheatRestore":
		return res(cheatRestore())
	case "npcGet":
		n, err := npcGet()
		if err != nil {
			return res(false, err.Error())
		}
		return apiResult{OK: true, Data: n}
	case "npcSet":
		return res(npcSet(a.ID, a.Value))
	case "npcMax":
		return res(npcMax())
	case "timeFF":
		st.mu.Lock()
		st.cheats["freeze"] = false // the helper unfreezes the clock for the fast-forward
		st.cheats["noon"] = false
		st.mu.Unlock()
		return res(timeFastForward(a.Target, a.Hour, a.Speed, a.Rested))
	case "timeSet":
		st.mu.Lock()
		st.cheats["noon"] = false // the helper drops "always noon" when the clock is set
		st.mu.Unlock()
		return res(timeSet(a.Hour, a.Minute))
	case "timeStop":
		return res(timeStop())
	case "zombieGet":
		z, err := zombieGet()
		if err != nil {
			return res(false, err.Error())
		}
		return apiResult{OK: true, Data: z}
	case "corpseGet":
		z, err := corpseGet()
		if err != nil {
			return res(false, err.Error())
		}
		return apiResult{OK: true, Data: z}
	case "catalog":
		c, err := zombieCatalog()
		if err != nil {
			return res(false, err.Error())
		}
		return apiResult{OK: true, Data: c}
	case "zombieCmd":
		return res(zombieCmd(a.Cmd))
	case "corpseCmd":
		return res(corpseCmd(a.Cmd))
	}
	return res(false, "unknown action "+action)
}

func clampWeekday(w int) int {
	if w < 1 || w > 6 {
		return 0
	}
	return w
}

// runWebUI shows the modern window. It returns false if WebView2 isn't available.
func runWebUI(items []Item) bool {
	dataDir, err := os.UserCacheDir()
	if err != nil {
		dataDir = os.TempDir()
	}
	// WebView2Loader.dll (Microsoft-signed) must be next to the .exe; it's loaded the normal way.
	if !webviewloader.Available() {
		return false
	}
	if v, err := webviewloader.GetInstalledVersion(); err != nil || v == "" {
		return false // WebView2 runtime not installed
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  filepath.Join(dataDir, "gk2_item_spawner", "webview"),
		WindowOptions: webview2.WindowOptions{
			Title:  "Graveyard Keeper 2 Trainer " + appVersion,
			Width:  1180,
			Height: 800,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		return false
	}
	defer w.Destroy()
	w.SetSize(980, 680, webview2.HintMin)

	st := &webState{Text: "Looking for the game…", Level: "err", cheats: map[string]bool{}, speeds: map[string]float64{}}
	go func() {
		for {
			st.connect()
			time.Sleep(3 * time.Second)
		}
	}()

	// Async bridge: the page calls gk2call(id, action, json) which returns at once; the work runs
	// in a goroutine and the answer comes back through window.__gk2done(id, result).
	_ = w.Bind("gk2call", func(id int, action string, args string) {
		go func() {
			r := handleAPI(st, items, action, args)
			b, err := json.Marshal(r)
			if err != nil {
				b, _ = json.Marshal(res(false, err.Error()))
			}
			js := fmt.Sprintf("window.__gk2done(%d,%s)", id, strings.ReplaceAll(string(b), " ", "\\u2028"))
			w.Dispatch(func() { w.Eval(js) })
		}()
	})
	w.SetHtml(webUIHTML)
	w.Run()
	return true
}
