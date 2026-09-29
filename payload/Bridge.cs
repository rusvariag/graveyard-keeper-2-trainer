// GK2Spawner: in-game helper loaded into Graveyard Keeper 2 by the Go launcher.
//
// It listens on 127.0.0.1:27817 for text commands:
//   PING                    -> PONG
//   VERSION                 -> VERSION <n>
//   ADD <itemId> <count>    -> OK ... / ERR ...
//   TECH <red> <green> <blue> -> OK tech points now ... (TECH 0 0 0 = read balance)
//   MONEY <delta>           -> OK money now ... (copper; negative removes, MONEY 0 = read balance)
//   INSTANT <1|0>           -> OK instant craft on/off (player crafts finish on the first hit)
//   ZOMBIE <sub> ...        -> zombie editor for the zombie menu open in game (see Zombie.cs)
//   CORPSE <sub> ...        -> dead-body editor: body on an open autopsy/embalm table or grave, or carried (Corpse.cs)
// Commands are queued and executed on Unity's main thread
// (Application.onBeforeRender), because game/Unity APIs are not thread-safe.

using System;
using System.Collections.Concurrent;
using System.IO;
using System.Net;
using System.Net.Sockets;
using System.Text;
using System.Threading;
using UnityEngine;

namespace GK2Spawner
{
    public static class Bridge
    {
        public const int Port = 27817;
        public const int Version = 6; // bump when the protocol changes; the launcher checks it
        private const int MaxCount = 9999;
        private const int TechCap = 999; // max of GameResSystemDef tech_red/green/blue
        private static readonly string[] TechRes = { "tech_red", "tech_green", "tech_blue" };
        private const int MoneyMax = 999999999; // GameResSystemDef "money" min 0 / max 999999999 (copper)

        private static int started;
        private static volatile bool instantCraft;
        private static string lastInstantError;
        private static readonly ConcurrentQueue<Command> Queue = new ConcurrentQueue<Command>();

        private sealed class Command
        {
            public string ItemId;
            public int Count;
            public int[] Tech; // red, green, blue to add (TECH command); null for ADD
            public int? Money; // copper to add/remove (MONEY command)
            public Func<string> Action; // any other main-thread job (ZOMBIE commands)
            public string Result;
            public readonly ManualResetEventSlim Done = new ManualResetEventSlim(false);
        }

        // Called once by the launcher through mono_runtime_invoke (on a foreign thread).
        public static void Start()
        {
            if (Interlocked.Exchange(ref started, 1) == 1)
            {
                return;
            }
            // Subscribing to a static event is safe from any thread; the handler runs on the main thread.
            Application.onBeforeRender += Pump;
            var listener = new Thread(Listen) { IsBackground = true, Name = "GK2Spawner" };
            listener.Start();
            Debug.Log("[GK2Spawner] started on 127.0.0.1:" + Port);
        }

        private static void Listen()
        {
            TcpListener server;
            try
            {
                server = new TcpListener(IPAddress.Loopback, Port);
                server.Start();
            }
            catch (Exception e)
            {
                Debug.LogError("[GK2Spawner] cannot listen: " + e.Message);
                return;
            }
            while (true)
            {
                try
                {
                    TcpClient client = server.AcceptTcpClient();
                    ThreadPool.QueueUserWorkItem(_ => Serve(client));
                }
                catch (Exception)
                {
                    Thread.Sleep(200);
                }
            }
        }

        private static void Serve(TcpClient client)
        {
            try
            {
                using (client)
                using (NetworkStream stream = client.GetStream())
                using (var reader = new StreamReader(stream, new UTF8Encoding(false)))
                using (var writer = new StreamWriter(stream, new UTF8Encoding(false)) { AutoFlush = true, NewLine = "\n" })
                {
                    string line;
                    while ((line = reader.ReadLine()) != null)
                    {
                        writer.WriteLine(Handle(line.Trim()));
                    }
                }
            }
            catch (Exception)
            {
                // client went away
            }
        }

        private static string Handle(string line)
        {
            string[] parts = line.Split(new[] { ' ' }, StringSplitOptions.RemoveEmptyEntries);
            if (parts.Length == 1 && parts[0] == "PING")
            {
                return "PONG";
            }
            if (parts.Length == 1 && parts[0] == "VERSION")
            {
                return "VERSION " + Version;
            }
            if (parts.Length >= 1 && parts[0] == "TECH")
            {
                return HandleTech(parts);
            }
            if (parts.Length >= 1 && parts[0] == "INSTANT")
            {
                if (parts.Length != 2 || (parts[1] != "1" && parts[1] != "0"))
                {
                    return "ERR usage: INSTANT 1|0";
                }
                instantCraft = parts[1] == "1"; // read by Pump on the main thread
                return "OK instant craft " + (instantCraft ? "on - one hit finishes a craft" : "off");
            }
            if (parts.Length >= 2 && parts[0] == "CORPSE")
            {
                string[] cargs = new string[parts.Length - 1];
                Array.Copy(parts, 1, cargs, 0, cargs.Length);
                return Run(new Command { Action = () => CorpseEditor.Handle(cargs) });
            }
            if (parts.Length >= 2 && parts[0] == "ZOMBIE")
            {
                string[] args = new string[parts.Length - 1];
                Array.Copy(parts, 1, args, 0, args.Length);
                int at = line.IndexOf(parts[1], "ZOMBIE".Length, StringComparison.Ordinal) + parts[1].Length;
                string rest = at < line.Length ? line.Substring(at).Trim() : "";
                return Run(new Command { Action = () => ZombieEditor.Handle(args, rest) });
            }
            if (parts.Length >= 1 && parts[0] == "MONEY")
            {
                int delta;
                if (parts.Length != 2 || !int.TryParse(parts[1], out delta) || delta < -MoneyMax || delta > MoneyMax)
                {
                    return "ERR usage: MONEY <copper> (negative removes, 0 = show)";
                }
                return Run(new Command { Money = delta });
            }
            if (parts.Length != 3 || parts[0] != "ADD")
            {
                return "ERR usage: ADD <itemId> <count> | TECH <red> <green> <blue> | MONEY <copper> | INSTANT 1|0 | ZOMBIE ...";
            }
            int count;
            if (!int.TryParse(parts[2], out count) || count < 1 || count > MaxCount)
            {
                return "ERR count must be 1-" + MaxCount;
            }
            return Run(new Command { ItemId = parts[1], Count = count });
        }

        private static string HandleTech(string[] parts)
        {
            int r, g, b;
            if (parts.Length != 4 || !int.TryParse(parts[1], out r) || !int.TryParse(parts[2], out g) || !int.TryParse(parts[3], out b)
                || r < 0 || g < 0 || b < 0 || r > TechCap || g > TechCap || b > TechCap)
            {
                return "ERR usage: TECH <red> <green> <blue> (0-" + TechCap + " each)";
            }
            return Run(new Command { Tech = new[] { r, g, b } });
        }

        private static string Run(Command cmd)
        {
            Queue.Enqueue(cmd);
            // The main thread only pumps while frames render; a paused/minimised game may delay this.
            return cmd.Done.Wait(10000) ? cmd.Result : "ERR timeout - is the game window running (not minimised)?";
        }

        // Runs on Unity's main thread every frame.
        private static void Pump()
        {
            Command cmd;
            while (Queue.TryDequeue(out cmd))
            {
                try
                {
                    cmd.Result = cmd.Action != null ? cmd.Action()
                        : cmd.Money.HasValue ? ChangeMoney(cmd.Money.Value)
                        : cmd.Tech != null ? AddTechPoints(cmd.Tech)
                        : Give(cmd.ItemId, cmd.Count);
                }
                catch (Exception e)
                {
                    cmd.Result = "ERR " + e.GetType().Name + ": " + e.Message;
                }
                cmd.Done.Set();
            }
            if (instantCraft)
            {
                FinishPlayerCraft();
            }
        }

        // Instant craft: while the player works at a station, push the current craft's
        // progress to the end - the same call a tool hit makes (CraftComponent.UpdateManual),
        // so the game finishes it normally (output, queue, XP) after its usual short delay.
        private static void FinishPlayerCraft()
        {
            try
            {
                if (MainGame.Instance == null || MainGame.PlayerController == null)
                {
                    return;
                }
                PlayerCraftActivity activity = MainGame.PlayerController.WorkerActivity as PlayerCraftActivity;
                CraftComponent craft = activity != null ? activity.CraftComponent : null;
                CraftElementBase element = craft != null ? craft.CurrentCraftElement : null;
                if (element == null || !element.IsStarted || element.ProgressTicks >= element.TotalProgressTicks)
                {
                    return;
                }
                if (element.ParamsData != null && element.ParamsData.craftParamsType == CraftParamsData.CraftParamsType.GardenGrowing)
                {
                    return; // plants grow on their own timer
                }
                if (!activity.IsEnoughMastery())
                {
                    return; // the game wouldn't let this hit count either
                }
                craft.UpdateManual(element.TotalProgressTicks - element.ProgressTicks);
                lastInstantError = null;
            }
            catch (Exception e)
            {
                if (lastInstantError != e.Message) // log once, not every frame
                {
                    lastInstantError = e.Message;
                    Debug.LogWarning("[GK2Spawner] instant craft: " + e);
                }
            }
        }

        private static string Give(string itemId, int count)
        {
            PlayerData player = MainGame.PlayerData;
            if (MainGame.Instance == null || player == null || player.Inventory == null)
            {
                return "ERR no save loaded - load your game first";
            }
            // Many real item ids contain ':' themselves (organs "heart_0_1:1", "grape_juice:2",
            // "body_certificate:1", ...), so look the id up exactly as given.
            if (GameBalance.Me == null || GameBalance.Me.GetDataOrNull<ItemDef>(itemId) == null)
            {
                return "ERR unknown item id '" + itemId + "'";
            }

            // Check space first so a partial add can never duplicate items.
            if (player.Inventory.CanAddItemToInventory(itemId, count) &&
                player.Inventory.AddItemToInventory(new Item(itemId, count)))
            {
                return "OK added " + count + " x " + itemId + " to inventory";
            }

            // Same call the game's own DropItem() quest reward uses: drop in front of the player.
            Vector3 pos = player.position.Value + new Vector3(player.Direction.x, 0f, player.Direction.y);
            MainGame.Instance.dropSystem.DropItem(new Item(itemId, count), player.currentGameSceneId, pos);
            return "OK inventory full - dropped " + count + " x " + itemId + " at your feet";
        }

        // Adds red/green/blue tech points straight to the player's balance (capped at 999 each).
        // "TECH 0 0 0" just reports the current balance.
        private static string AddTechPoints(int[] add)
        {
            PlayerData player = MainGame.PlayerData;
            if (MainGame.Instance == null || player == null)
            {
                return "ERR no save loaded - load your game first";
            }
            var now = new int[3];
            for (int i = 0; i < 3; i++)
            {
                GameResSystemDef def = GameBalance.Me != null ? GameBalance.Me.GetDataOrNull<GameResSystemDef>(TechRes[i]) : null;
                string resId = def != null ? def.ResId : TechRes[i];
                int current = player.GetResInt(resId);
                int amount = Math.Max(0, Math.Min(add[i], TechCap - current));
                if (amount > 0)
                {
                    player.AddRes(resId, amount); // raises OnGameResChanged so the HUD updates
                }
                now[i] = player.GetResInt(resId);
            }
            return "OK tech points now red " + now[0] + ", green " + now[1] + ", blue " + now[2];
        }

        // Money is the player res "money", counted in copper (1 silver = 100, 1 gold = 10000).
        private static string ChangeMoney(int delta)
        {
            PlayerData player = MainGame.PlayerData;
            if (MainGame.Instance == null || player == null)
            {
                return "ERR no save loaded - load your game first";
            }
            GameResSystemDef def = GameBalance.Me != null ? GameBalance.Me.GetDataOrNull<GameResSystemDef>("money") : null;
            string resId = def != null ? def.ResId : "money";
            long current = player.GetResInt(resId);
            long target = Math.Max(0L, Math.Min((long)MoneyMax, current + delta));
            if (target != current)
            {
                player.AddRes(resId, (float)(target - current)); // raises OnGameResChanged so the HUD updates
            }
            int now = player.GetResInt(resId);
            string change = delta == 0 ? "" : (now - current >= 0 ? " (+" : " (") + (now - current) + ")";
            return "OK money now " + FormatCopper(now) + change;
        }

        private static string FormatCopper(long copper)
        {
            return (copper / 10000) + "g " + (copper / 100 % 100) + "s " + (copper % 100) + "c";
        }
    }
}
