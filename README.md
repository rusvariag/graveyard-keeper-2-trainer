# GK2 Item Spawner

A small Windows tool for **single-player** Graveyard Keeper 2:
- pick any item from a searchable list, choose a count, and it appears in your inventory (if it doesn't fit, it's dropped at your feet).
- add **red / green / blue tech points** (research points; the game caps each colour at 999).
- **add or remove money** in gold / silver / copper (1 gold = 100 silver = 10,000 copper).
- **instant craft**: while it's on, the first hit at a workbench finishes the craft.
- **zombie editor**: while a zombie's menu is open in the game, edit that zombie's name, skill-tree points, organs (and so its white/red skulls), body items, collar, tool/weapon, armour and skill tree.

## Use it
1. Run `gk2_item_spawner.exe`.
1. Start the game (or have it running) and **load your save**. The tool finds the game and **connects by itself**, and the status dot turns green. Use **Reconnect** only if you want to retry straight away.
1. Items: type in the search box, pick an item, set the count (or use ×1 / ×10 / ×50 / Stack), and click **Add to inventory**. You can also double-click the item.
1. Tech points: enter red / green / blue amounts and click **Add**. **Show** displays your current balance.
1. Money: enter gold / silver / copper and click **Add** or **Remove**. **Show** displays your current money. Removing more than you have leaves you at 0.
1. Crafting: turn on **Instant craft**, then craft as usual. The first hit finishes the item, and each queued item takes one hit. It applies only to what *you* craft, not to zombies or growing plants. It stays on after a game restart, because the tool re-sends it when it reconnects.
1. Zombie: in the game, open a zombie's menu (the one where you give it items or spend its red/green/blue points). The **Edit zombie…** button lights up within a second. Click it to open the editor:
   - **Name**: type a name and click **Rename**.
   - **Points**: set the zombie's red / green / blue skill-tree points.
   - **Equipment**: pick the collar (bronze up to 5 red skulls, gold up to 10, steel up to 99), the tool or weapon, and the armour. "(empty)" removes a tool or armour.
   - **Body**: lists every organ and item inside the zombie with its skull value. Remove one, **Replace** an organ with another of the same kind (for example a better heart), or **Add** organs and embalming items. White and red skulls aren't a number you can type in: the game adds them up from these items, so this is how you change them.
   - **Skill tree**: pick a branch, then **Learn** or **Forget** skills. Learning is free. The game allows as many active skills as the zombie has red skulls; learned skills above that show as inactive until you add red skulls.
   - The game's zombie menu redraws after each change. If something looks stale, close and reopen it, then reload.

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
- **Tech points.** `TECH r g b` calls `PlayerData.AddRes("tech_red"/"tech_green"/"tech_blue", n)`, the resources behind the research-point orbs. It's clamped at 999, and the HUD updates through the game's `OnGameResChanged` event.
- **Money.** `MONEY <copper>` (negative removes) changes the player resource `money`, which the game stores in copper, within the game's own limits of 0 to 999,999,999.
- **Instant craft.** `INSTANT 1|0` sets a flag. Every frame, while you work at a station, the helper tops up the current craft's progress with `CraftComponent.UpdateManual(remaining)`, the same call a tool hit makes. The game then finishes the craft normally: output, queue and XP. Crafts that need more mastery than you have are left alone.
- **Zombie editor.** `ZOMBIE <sub>` works on the zombie shown in the game's `UIZombieWorkerWindow`, which the helper finds and reads through the window's `data`.
  - Sub-commands: `STATE`, `GET` and `CATALOG` (one-line JSON), `NAME`, `TECH r g b`, `ADD id n`, `REMOVE uid`, `REPLACE uid id`, `EQUIP collar|hand|armor id`, `UNEQUIP hand|armor`, `PERK id 1|0`.
  - Body items are added and removed through the window's body inventory, followed by `OnAddOrgan`/`OnRemoveOrgan` (organ-linked perks and red-skull slots).
  - Equipment is set by adding the item to the body and writing its id into `equippedCollar/Hand/Armor`, then calling the same "tool changed" hook as the game.
  - A skill is learned with `PurchaseTalentLevelUp(def, free)` followed by `CheckRedSkulls`. Afterwards the window is redrawn with its own data.

## Build from source
```bash
# In-game helper (needs the .NET SDK; references the game's DLLs in ../GraveyardKeeper2_Data/Managed)
dotnet build payload -c Release -o build

# Windows manifest/version resource (only needed if winres/winres.json changes)
go install github.com/tc-hib/go-winres@latest && go-winres make --in winres/winres.json --arch amd64

# The tool (Go 1.22+), cross-compiled for Windows x64; it embeds build/GK2Spawner.dll
GOOS=windows GOARCH=amd64 go build -buildvcs=false -trimpath -ldflags="-s -w -H windowsgui" -o gk2_item_spawner.exe .
```
Files: `core.go` (item list and helper protocol), `gui_windows.go` (the window), `gui_zombie_windows.go` (the zombie / dead body editor), `inject_windows.go` (Mono injection), `payload/` (the in-game helper).
A non-Windows build is a small command-line tool for testing against the helper (run it without arguments for the list of commands).

## Notes
- It builds for Windows as a GUI app with its manifest embedded, the protocol is exercised against a stand-in helper, and the helper compiles against the real game DLLs. It has **not** been run against the live game on Windows yet, so treat the first run as a test and **back up your save** first.
- After a game update, rebuild the helper, and regenerate `items.json` if items changed.
- For single-player use only. The helper only accepts connections from your own PC.
