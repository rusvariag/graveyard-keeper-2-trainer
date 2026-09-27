# GK2 Item Spawner

A small Windows tool for **single-player** Graveyard Keeper 2:
- pick any item from a searchable list, choose a count, and it appears in your inventory (if it doesn't fit, it's dropped at your feet).

## Use it
1. Run `gk2_item_spawner.exe`.
1. Start the game (or have it running) and **load your save**. The tool finds the game and **connects by itself**, and the status dot turns green. Use **Reconnect** only if you want to retry straight away.
1. Items: type in the search box, pick an item, set the count (or use ×1 / ×10 / ×50 / Stack), and click **Add to inventory**. You can also double-click the item.

After **updating this tool**, restart the game once, because the old helper can't be unloaded. The status line tells you when that's needed (yellow dot).

## How it works
```
gk2_item_spawner.exe ─(1) injects GK2Spawner.dll via the Mono API──►  GraveyardKeeper2.exe
     └──(2) "ADD iron_ingot 5" over 127.0.0.1:27817  ──────────►   │ Bridge.Start() → queue → main thread
                                                                    │ PlayerData.Inventory.AddItemToInventory(new Item(id, n))
                                                                    │ or dropSystem.DropItem(...) if full
```
- **Injection** (`inject_windows.go`). Graveyard Keeper 2 runs on Unity's **Mono** runtime. The tool finds `mono-2.0-bdwgc.dll` in the game process and calls Mono's own embedding functions in remote threads, using a small x64 stub: `mono_get_root_domain` → `mono_domain_assembly_open` → `mono_class_from_name` → `mono_runtime_invoke(Bridge.Start)`. Each remote thread attaches to Mono before the call and detaches after it.
- **In-game helper** (`payload/Bridge.cs`). It listens on **127.0.0.1 only** and runs every command on **Unity's main thread** through `Application.onBeforeRender`. That matters because the game's inventory and UI code isn't thread-safe.
- **Safe adds.** The helper checks `CanAddItemToInventory(id, count)` first. If the items fit, they go into the inventory; otherwise it uses the same drop call as the game's own `DropItem` quest rewards, so a partial add can never duplicate items.
- **Items list** (`items.json`). All 812 item ids, with English names and stack sizes, taken from the game's `GameBalance` and `lng_en`. Many real item ids contain `:` (organs such as `heart_2_2:2`, `grape_juice:3`, `body_certificate:1`). The helper looks ids up exactly as written.

## Build from source
```bash
# In-game helper (needs the .NET SDK; references the game's DLLs in ../GraveyardKeeper2_Data/Managed)
dotnet build payload -c Release -o build

# Windows manifest/version resource (only needed if winres/winres.json changes)
go install github.com/tc-hib/go-winres@latest && go-winres make --in winres/winres.json --arch amd64

# The tool (Go 1.22+), cross-compiled for Windows x64; it embeds build/GK2Spawner.dll
GOOS=windows GOARCH=amd64 go build -buildvcs=false -trimpath -ldflags="-s -w -H windowsgui" -o gk2_item_spawner.exe .
```
Files: `core.go` (item list and helper protocol), `gui_windows.go` (the window), `inject_windows.go` (Mono injection), `payload/` (the in-game helper).
A non-Windows build is a small command-line tool for testing against the helper (run it without arguments for the list of commands).

## Notes
- It builds for Windows as a GUI app with its manifest embedded, the protocol is exercised against a stand-in helper, and the helper compiles against the real game DLLs. It has **not** been run against the live game on Windows yet, so treat the first run as a test and **back up your save** first.
- After a game update, rebuild the helper, and regenerate `items.json` if items changed.
- For single-player use only. The helper only accepts connections from your own PC.
