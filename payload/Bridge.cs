// GK2Spawner: in-game helper loaded into Graveyard Keeper 2 by the Go launcher.
//
// It listens on 127.0.0.1:27817 for text commands:
//   PING                    -> PONG
//   VERSION                 -> VERSION <n>
//   ADD <itemId> <count>    -> OK ... / ERR ...
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
        public const int Version = 1; // bump when the protocol changes; the launcher checks it
        private const int MaxCount = 9999;

        private static int started;
        private static readonly ConcurrentQueue<Command> Queue = new ConcurrentQueue<Command>();

        private sealed class Command
        {
            public string ItemId;
            public int Count;
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
            if (parts.Length != 3 || parts[0] != "ADD")
            {
                return "ERR usage: ADD <itemId> <count>";
            }
            int count;
            if (!int.TryParse(parts[2], out count) || count < 1 || count > MaxCount)
            {
                return "ERR count must be 1-" + MaxCount;
            }
            return Run(new Command { ItemId = parts[1], Count = count });
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
                    cmd.Result = Give(cmd.ItemId, cmd.Count);
                }
                catch (Exception e)
                {
                    cmd.Result = "ERR " + e.GetType().Name + ": " + e.Message;
                }
                cmd.Done.Set();
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

    }
}
