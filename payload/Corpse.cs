// Dead-body editor: changes the organs and embalming items of a corpse before (or after) burial.
// Runs on Unity's main thread (see Bridge.Pump).
//
// A body is an Item in the "body" group; its Inventory holds the organs and the pocket items
// (embalming solutions). Its white/red skulls are summed from those items, and a grave's quality is
//     grave items' quality - body red skulls, capped at body white skulls   (Inventory.GetTotalQualityGrave)
// so white skulls raise the ceiling and red skulls are a penalty.
//
// Where the body is taken from, in this order: an open Autopsy table, Embalming table or Grave
// window; otherwise a body the player carries on the shoulders.

using System;
using System.Collections.Generic;
using System.Reflection;
using LazyBearTechnology;
using UnityEngine;

namespace GK2Spawner
{
    public static class CorpseEditor
    {
        private sealed class Target
        {
            public Item Body;
            public string Where;      // autopsy | embalm | grave | carried
            public WgoData Host;      // table / grave holding the body (null when carried)
            public ZombieWgoData Zombie;
            public Action Redraw;     // refresh the open game window
        }

        private static readonly Dictionary<Type, UnityEngine.Object> Windows = new Dictionary<Type, UnityEngine.Object>();

        private static T FindWindow<T>() where T : UnityEngine.Object
        {
            UnityEngine.Object w;
            if (!Windows.TryGetValue(typeof(T), out w) || w == null)
            {
                T[] all = UnityEngine.Object.FindObjectsByType<T>(FindObjectsInactive.Include, FindObjectsSortMode.None);
                w = all.Length > 0 ? all[0] : null;
                Windows[typeof(T)] = w;
            }
            return w as T;
        }

        private static object Field(object obj, string name)
        {
            for (Type t = obj.GetType(); t != null; t = t.BaseType)
            {
                FieldInfo f = t.GetField(name, BindingFlags.Instance | BindingFlags.NonPublic | BindingFlags.Public | BindingFlags.DeclaredOnly);
                if (f != null)
                {
                    return f.GetValue(obj);
                }
            }
            return null;
        }

        private static Target Find()
        {
            if (MainGame.Instance == null || MainGame.PlayerData == null)
            {
                return null;
            }
            UIAutopsyWindow autopsy = FindWindow<UIAutopsyWindow>();
            if (autopsy != null && autopsy.IsShown)
            {
                var data = Field(autopsy, "data") as UIAutopsyWindowData;
                var table = data != null ? Field(data, "autopsyTable") as WgoData : null;
                if (data != null && !data.IsEmpty && data.CorpseWidgetData != null && data.CorpseWidgetData.Body != null)
                {
                    return Make(data.CorpseWidgetData.Body, "autopsy", table, () => { if (table != null) autopsy.Draw(new UIAutopsyWindowData(table)); });
                }
            }
            UIEmbalmWindow embalm = FindWindow<UIEmbalmWindow>();
            if (embalm != null && embalm.IsShown)
            {
                var data = Field(embalm, "data") as UIEmbalmWindowData;
                var table = data != null ? Field(data, "table") as WgoData : null;
                if (data != null && !data.IsEmpty && data.CorpseWidgetData != null && data.CorpseWidgetData.Body != null)
                {
                    return Make(data.CorpseWidgetData.Body, "embalm", table, () => { if (table != null) embalm.Draw(new UIEmbalmWindowData(table)); });
                }
            }
            UIGraveWindow grave = FindWindow<UIGraveWindow>();
            if (grave != null && grave.IsShown)
            {
                var data = Field(grave, "data") as UIGraveWindowData;
                if (data != null && data.CorpseWidgetData != null && !data.CorpseWidgetData.IsEmpty && data.CorpseWidgetData.Body != null)
                {
                    WgoData host = data.WgoData;
                    return Make(data.CorpseWidgetData.Body, "grave", host, () =>
                    {
                        Wgo view = host != null ? GameScene.GetWgoViewGlobal(host.UniqueId) : null;
                        if (view != null)
                        {
                            grave.Draw(new UIGraveWindowData(view));
                            view.DrawWidgets();
                        }
                    });
                }
            }
            IReadOnlyList<Item> carried = MainGame.PlayerData.OverheadItems;
            if (carried != null)
            {
                for (int i = carried.Count - 1; i >= 0; i--)
                {
                    Item it = carried[i];
                    if (it != null && !it.IsEmpty && it.Definition != null && it.Definition.itemGroupIds.Contains("body"))
                    {
                        return Make(it, "carried", null, () => { });
                    }
                }
            }
            return null;
        }

        private static Target Make(Item body, string where, WgoData host, Action redraw)
        {
            return new Target
            {
                Body = body,
                Where = where,
                Host = host,
                Zombie = MainGame.ZombieSystemData != null ? MainGame.ZombieSystemData.GetZombie(body.UniqueId) : null,
                Redraw = redraw,
            };
        }

        public static string Handle(string[] args)
        {
            string sub = args.Length > 0 ? args[0].ToUpperInvariant() : "";
            Target t = Find();
            if (sub == "STATE")
            {
                return t == null ? "OK none" : "OK open " + t.Where + " " + BodyName(t);
            }
            if (t == null)
            {
                return "ERR open an autopsy table, embalming table or grave with a body, or carry a body";
            }
            string result;
            switch (sub)
            {
                case "GET":
                    return "OK " + Describe(t);
                case "ADD":
                    result = Add(t, args);
                    break;
                case "REMOVE":
                    result = args.Length == 2 ? Remove(t, args[1]) : "ERR usage: CORPSE REMOVE <uid>";
                    break;
                case "REPLACE":
                    result = args.Length == 3 ? Replace(t, args[1], args[2]) : "ERR usage: CORPSE REPLACE <uid> <itemId>";
                    break;
                default:
                    return "ERR usage: CORPSE STATE|GET|ADD id [n]|REMOVE uid|REPLACE uid id";
            }
            if (result.StartsWith("OK"))
            {
                try
                {
                    t.Redraw();
                }
                catch (Exception e)
                {
                    Debug.LogWarning("[GK2Spawner] body window redraw: " + e.Message);
                }
            }
            return result;
        }

        // ---------- skulls ----------

        private static void Skulls(Item body, out int white, out int red)
        {
            white = 0;
            red = 0;
            foreach (Item it in body.Inventory)
            {
                if (it == null || it.IsEmpty) continue;
                white += it.Definition.whiteSkulls * it.Count;
                red += it.Definition.redSkulls * it.Count;
            }
            white = Mathf.Clamp(white, 0, 999);
            red = Mathf.Clamp(red, 0, 999);
        }

        private static string SkullText(Item body)
        {
            int w, r;
            Skulls(body, out w, out r);
            return "white " + w + ", red " + r;
        }

        private static string BodyName(Target t)
        {
            if (t.Zombie != null && !string.IsNullOrEmpty(t.Zombie.Name)) return t.Zombie.Name;
            string n = "";
            try { n = LLBase.L(t.Body.id); } catch (Exception) { }
            return string.IsNullOrEmpty(n) || n == t.Body.id ? "body" : n;
        }

        // ---------- edits ----------

        private static bool IsZombieEquipment(Target t, Item it)
        {
            ZombieWgoData z = t.Zombie;
            return z != null && ((!z.equippedCollar.IsEmpty && z.equippedCollar == it.UniqueId)
                              || (!z.equippedHand.IsEmpty && z.equippedHand == it.UniqueId)
                              || (!z.equippedArmor.IsEmpty && z.equippedArmor == it.UniqueId));
        }

        private static Item FindInBody(Target t, string uid)
        {
            foreach (Item it in t.Body.Inventory)
            {
                if (it != null && (it.UniqueId.ToString() == uid || it.UniqueId.Id == uid)) return it;
            }
            return null;
        }

        private static string Add(Target t, string[] args)
        {
            int count = 1;
            if (args.Length < 2 || args.Length > 3 || (args.Length == 3 && (!int.TryParse(args[2], out count) || count < 1 || count > 99)))
            {
                return "ERR usage: CORPSE ADD <itemId> [count 1-99]";
            }
            ItemDef def = GameBalance.Me.GetDataOrNull<ItemDef>(args[1]);
            if (def == null)
            {
                return "ERR unknown item id '" + args[1] + "'";
            }
            List<Item> added;
            if (!new Inventory(t.Body).TryAddItemToInventory(new Item(args[1], count), out added) || added == null || added.Count == 0)
            {
                return "ERR the body has no room for " + args[1];
            }
            if (t.Zombie != null) foreach (Item it in added) t.Zombie.OnAddOrgan(it);
            return "OK added " + count + " x " + args[1] + " - skulls now " + SkullText(t.Body);
        }

        private static string Remove(Target t, string uid)
        {
            Item it = FindInBody(t, uid);
            if (it == null) return "ERR item not found in the body";
            if (IsZombieEquipment(t, it)) return "ERR that's the zombie's equipment - use the zombie editor";
            string id = it.id;
            if (!new Inventory(t.Body).RemoveItemFromInventoryByUID(it)) return "ERR the game refused to remove " + id;
            if (t.Zombie != null) t.Zombie.OnRemoveOrgan(it);
            return "OK removed " + id + " - skulls now " + SkullText(t.Body);
        }

        private static string Replace(Target t, string uid, string newId)
        {
            Item old = FindInBody(t, uid);
            if (old == null) return "ERR item not found in the body";
            if (IsZombieEquipment(t, old)) return "ERR that's the zombie's equipment - use the zombie editor";
            if (GameBalance.Me.GetDataOrNull<ItemDef>(newId) == null) return "ERR unknown item id '" + newId + "'";
            int count = old.Count;
            string oldId = old.id;
            Inventory body = new Inventory(t.Body);
            if (!body.RemoveItemFromInventoryByUID(old)) return "ERR the game refused to remove " + oldId;
            if (t.Zombie != null) t.Zombie.OnRemoveOrgan(old);
            List<Item> added;
            if (!body.TryAddItemToInventory(new Item(newId, count), out added) || added == null || added.Count == 0)
            {
                body.TryAddItemToInventory(new Item(oldId, count), out added); // put the old one back
                if (t.Zombie != null && added != null) foreach (Item it in added) t.Zombie.OnAddOrgan(it);
                return "ERR no room for " + newId + " - kept " + oldId;
            }
            if (t.Zombie != null) foreach (Item it in added) t.Zombie.OnAddOrgan(it);
            return "OK " + oldId + " -> " + newId + " - skulls now " + SkullText(t.Body);
        }

        // ---------- JSON out (same shape as ZOMBIE GET, so the app reuses its body editor) ----------

        private static string Describe(Target t)
        {
            int w, r;
            Skulls(t.Body, out w, out r);
            var j = new ZombieEditor.Json();
            j.Begin();
            j.Str("name", BodyName(t)).Str("type", t.Zombie != null ? "Zombie body" : "Corpse").Str("state", t.Where);
            j.Str("bodyId", t.Body.id).Int("white", w).Int("red", r);
            if (t.Where == "grave" && t.Host != null)
            {
                j.Int("graveQuality", Mathf.RoundToInt(t.Host.Quality));
            }
            j.Arr("items");
            foreach (Item it in t.Body.Inventory)
            {
                if (it == null || it.IsEmpty) continue;
                ItemDef d = it.Definition;
                j.Obj().Str("uid", it.UniqueId.ToString()).Str("id", it.id).Int("count", it.Count)
                    .Int("white", d.whiteSkulls * it.Count).Int("red", d.redSkulls * it.Count)
                    .Str("slot", IsZombieEquipment(t, it) ? "equipment" : "").Str("group", ZombieEditor.OrganGroup(d)).Str("perk", d.bodyLinkedPerk ?? "").End();
            }
            j.EndArr();
            j.End();
            return j.ToString();
        }
    }
}
