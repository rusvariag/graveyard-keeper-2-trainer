// Zombie editor: reads and changes the zombie shown in the game's zombie window
// (UIZombieWorkerWindow). Every method here runs on Unity's main thread (see Bridge.Pump).
//
// How the game stores a zombie (ZombieWgoData):
//   name, techRed/Green/Blue                 - plain fields
//   ZombieItem.Inventory                      - the body: organs, embalming items, pocket items AND
//                                               the equipped collar / tool / armour items
//   equippedCollar / equippedHand / equippedArmor - unique ids of those items inside the body
//   white / red skulls                        - NOT stored: summed from ItemDef.whiteSkulls/redSkulls
//                                               of everything in the body
//   talentData[branch].studiedLevelUps        - bought skill-tree nodes; nodes above the red-skull
//                                               count are listed in disabledTalentLevelUps

using System;
using System.Collections.Generic;
using System.Globalization;
using System.Reflection;
using System.Text;
using LazyBearTechnology;
using UnityEngine;

namespace GK2Spawner
{
    public static class ZombieEditor
    {
        private const int TechMax = 99999;
        private static UIZombieWorkerWindow window;
        private static FieldInfo dataField;

        // ---------- finding the open zombie window ----------

        private static UIZombieWorkerWindow Window()
        {
            if (window == null)
            {
                UIZombieWorkerWindow[] all = UnityEngine.Object.FindObjectsByType<UIZombieWorkerWindow>(FindObjectsInactive.Include, FindObjectsSortMode.None);
                window = all.Length > 0 ? all[0] : null;
            }
            return window;
        }

        private static UIZombieWorkerWindowData OpenData()
        {
            UIZombieWorkerWindow w = Window();
            if (w == null || !w.IsShown)
            {
                return null;
            }
            if (dataField == null)
            {
                // LazyWidget<T>.data is protected; read it by reflection.
                for (Type t = w.GetType(); t != null && dataField == null; t = t.BaseType)
                {
                    dataField = t.GetField("data", BindingFlags.Instance | BindingFlags.NonPublic | BindingFlags.Public | BindingFlags.DeclaredOnly);
                }
            }
            return dataField == null ? null : dataField.GetValue(w) as UIZombieWorkerWindowData;
        }

        private static ZombieWgoData Current(out string error)
        {
            error = null;
            if (MainGame.Instance == null || MainGame.PlayerData == null)
            {
                error = "ERR no save loaded - load your game first";
                return null;
            }
            UIZombieWorkerWindowData data = OpenData();
            if (data == null || data.ZombieWgoData == null)
            {
                error = "ERR open a zombie's menu in the game first";
                return null;
            }
            return data.ZombieWgoData;
        }

        // Re-draw the open window with its own data object so the game's UI shows the change.
        private static void RefreshWindow()
        {
            try
            {
                UIZombieWorkerWindowData data = OpenData();
                if (data != null)
                {
                    window.Draw(data);
                }
            }
            catch (Exception e)
            {
                Debug.LogWarning("[GK2Spawner] zombie window redraw: " + e.Message);
            }
        }

        // ---------- command entry point ----------

        // args: the words after "ZOMBIE"; rest: the raw text after the sub-command (for names with spaces)
        public static string Handle(string[] args, string rest)
        {
            string sub = args.Length > 0 ? args[0].ToUpperInvariant() : "";
            if (sub == "CATALOG")
            {
                return Catalog();
            }
            string error;
            if (sub == "STATE")
            {
                if (MainGame.Instance == null || MainGame.PlayerData == null)
                {
                    return "OK none";
                }
                UIZombieWorkerWindowData d = OpenData();
                return d == null || d.ZombieWgoData == null ? "OK none" : "OK open " + (d.ZombieWgoData.Name ?? "");
            }
            ZombieWgoData z = Current(out error);
            if (z == null)
            {
                return error;
            }
            string result;
            switch (sub)
            {
                case "GET":
                    return "OK " + Describe(z);
                case "NAME":
                    result = SetName(z, rest);
                    break;
                case "TECH":
                    result = SetTech(z, args);
                    break;
                case "ADD":
                    result = AddBodyItem(z, args);
                    break;
                case "REMOVE":
                    result = args.Length == 2 ? RemoveBodyItem(z, args[1]) : "ERR usage: ZOMBIE REMOVE <uid>";
                    break;
                case "REPLACE":
                    result = args.Length == 3 ? ReplaceBodyItem(z, args[1], args[2]) : "ERR usage: ZOMBIE REPLACE <uid> <itemId>";
                    break;
                case "EQUIP":
                    result = args.Length == 3 ? Equip(z, args[1].ToLowerInvariant(), args[2]) : "ERR usage: ZOMBIE EQUIP collar|hand|armor <itemId>";
                    break;
                case "UNEQUIP":
                    result = args.Length == 2 ? Unequip(z, args[1].ToLowerInvariant()) : "ERR usage: ZOMBIE UNEQUIP hand|armor";
                    break;
                case "PERK":
                    result = args.Length == 3 && (args[2] == "1" || args[2] == "0") ? SetPerk(z, args[1], args[2] == "1") : "ERR usage: ZOMBIE PERK <levelUpId> 1|0";
                    break;
                default:
                    return "ERR usage: ZOMBIE STATE|GET|CATALOG|NAME|TECH|ADD|REMOVE|REPLACE|EQUIP|UNEQUIP|PERK";
            }
            if (result.StartsWith("OK"))
            {
                RefreshWindow();
            }
            return result;
        }

        // ---------- edits ----------

        private static string SetName(ZombieWgoData z, string name)
        {
            name = (name ?? "").Trim();
            if (name.Length == 0 || name.Length > 40)
            {
                return "ERR name must be 1-40 characters";
            }
            z.SetName(name, false);
            return "OK name is now " + name;
        }

        private static string SetTech(ZombieWgoData z, string[] args)
        {
            int r, g, b;
            if (args.Length != 4 || !int.TryParse(args[1], out r) || !int.TryParse(args[2], out g) || !int.TryParse(args[3], out b)
                || r < 0 || g < 0 || b < 0 || r > TechMax || g > TechMax || b > TechMax)
            {
                return "ERR usage: ZOMBIE TECH <red> <green> <blue> (0-" + TechMax + ")";
            }
            z.techRed = r;
            z.techGreen = g;
            z.techBlue = b;
            return "OK zombie points now red " + r + ", green " + g + ", blue " + b;
        }

        private static ItemDef Def(string id)
        {
            return GameBalance.Me == null ? null : GameBalance.Me.GetDataOrNull<ItemDef>(id);
        }

        private static bool IsEquipment(ItemDef d)
        {
            return d.type == ItemType.Collar || d.type == ItemType.BodyArmor || ((d.isTool || d.isWeapon) && d.type != ItemType.Sword);
        }

        private static Item FindInBody(ZombieWgoData z, string uid)
        {
            foreach (Item it in z.ZombieItem.Inventory)
            {
                if (it.UniqueId.ToString() == uid || it.UniqueId.Id == uid)
                {
                    return it;
                }
            }
            return null;
        }

        private static string SlotOf(ZombieWgoData z, Item it)
        {
            if (!z.equippedCollar.IsEmpty && z.equippedCollar == it.UniqueId) return "collar";
            if (!z.equippedHand.IsEmpty && z.equippedHand == it.UniqueId) return "hand";
            if (!z.equippedArmor.IsEmpty && z.equippedArmor == it.UniqueId) return "armor";
            return "";
        }

        // Adds to the body through the open window's own Inventory wrapper so the window's
        // add/remove events fire (skull labels, talent icons), like a drag & drop in game.
        private static Inventory BodyInventory(ZombieWgoData z)
        {
            UIZombieWorkerWindowData data = OpenData();
            if (data != null && data.BodyOrgansInventoryWidgetData != null && data.BodyOrgansInventoryWidgetData.Inventory != null)
            {
                return data.BodyOrgansInventoryWidgetData.Inventory;
            }
            return new Inventory(z.ZombieItem);
        }

        private static string AddBodyItem(ZombieWgoData z, string[] args)
        {
            int count = 1;
            if (args.Length < 2 || args.Length > 3 || (args.Length == 3 && (!int.TryParse(args[2], out count) || count < 1 || count > 99)))
            {
                return "ERR usage: ZOMBIE ADD <itemId> [count 1-99]";
            }
            ItemDef def = Def(args[1]);
            if (def == null)
            {
                return "ERR unknown item id '" + args[1] + "'";
            }
            if (IsEquipment(def))
            {
                return "ERR use EQUIP for collars, tools, weapons and armour";
            }
            List<Item> added;
            if (!BodyInventory(z).TryAddItemToInventory(new Item(args[1], count), out added) || added == null || added.Count == 0)
            {
                return "ERR the zombie's body has no room for " + args[1];
            }
            foreach (Item it in added)
            {
                z.OnAddOrgan(it); // skull-linked perks and red-skull perk slots, as the game does
            }
            return "OK added " + count + " x " + args[1] + " - skulls now white " + z.WhiteSkulls + ", red " + z.RedSkulls;
        }

        private static string RemoveBodyItem(ZombieWgoData z, string uid)
        {
            Item it = FindInBody(z, uid);
            if (it == null)
            {
                return "ERR item not found in the zombie's body";
            }
            string slot = SlotOf(z, it);
            if (slot == "collar")
            {
                return "ERR a zombie always needs a collar - use EQUIP collar to change it";
            }
            string id = it.id;
            if (!BodyInventory(z).RemoveItemFromInventoryByUID(it))
            {
                return "ERR the game refused to remove " + id;
            }
            if (slot == "hand") z.equippedHand.SetGuid(SGuid.Empty);
            else if (slot == "armor") z.equippedArmor.SetGuid(SGuid.Empty);
            else z.OnRemoveOrgan(it);
            if (slot != "") ToolChanged(z);
            return "OK removed " + id + " - skulls now white " + z.WhiteSkulls + ", red " + z.RedSkulls;
        }

        private static string ReplaceBodyItem(ZombieWgoData z, string uid, string newId)
        {
            Item old = FindInBody(z, uid);
            if (old == null)
            {
                return "ERR item not found in the zombie's body";
            }
            ItemDef def = Def(newId);
            if (def == null)
            {
                return "ERR unknown item id '" + newId + "'";
            }
            string slot = SlotOf(z, old);
            if (slot != "")
            {
                return Equip(z, slot, newId);
            }
            int count = old.Count;
            string oldId = old.id;
            Inventory body = BodyInventory(z);
            if (!body.RemoveItemFromInventoryByUID(old))
            {
                return "ERR the game refused to remove " + oldId;
            }
            z.OnRemoveOrgan(old);
            List<Item> added;
            if (!body.TryAddItemToInventory(new Item(newId, count), out added) || added == null || added.Count == 0)
            {
                body.TryAddItemToInventory(new Item(oldId, count), out added); // put the old one back
                if (added != null) foreach (Item it in added) z.OnAddOrgan(it);
                return "ERR no room for " + newId + " - kept " + oldId;
            }
            foreach (Item it in added)
            {
                z.OnAddOrgan(it);
            }
            return "OK " + oldId + " -> " + newId + " - skulls now white " + z.WhiteSkulls + ", red " + z.RedSkulls;
        }

        private static string Equip(ZombieWgoData z, string slot, string id)
        {
            ItemDef def = Def(id);
            if (def == null)
            {
                return "ERR unknown item id '" + id + "'";
            }
            bool ok = slot == "collar" ? def.type == ItemType.Collar
                    : slot == "armor" ? def.type == ItemType.BodyArmor
                    : slot == "hand" && (def.isTool || def.isWeapon) && def.type != ItemType.Sword;
            if (!ok)
            {
                return "ERR " + id + " can't go in the " + slot + " slot";
            }
            Item current = slot == "collar" ? z.Collar : slot == "armor" ? z.Armor : z.Hand;
            Inventory body = BodyInventory(z);
            if (current != null && !current.IsEmpty)
            {
                body.RemoveItemFromInventoryByUID(current, 1);
            }
            List<Item> added;
            if (!body.TryAddItemToInventory(new Item(id), out added) || added == null || added.Count == 0)
            {
                if (current != null && !current.IsEmpty && body.TryAddItemToInventory(new Item(current.id), out added) && added.Count > 0)
                {
                    SetSlot(z, slot, added[0].UniqueId); // restore the previous item
                }
                return "ERR the zombie's body has no room for " + id;
            }
            SetSlot(z, slot, added[0].UniqueId);
            body.ForceTriggerOnItemsAddEventWithoutItems();
            ToolChanged(z);
            return "OK " + slot + " is now " + id;
        }

        private static string Unequip(ZombieWgoData z, string slot)
        {
            if (slot != "hand" && slot != "armor")
            {
                return "ERR only hand and armor can be emptied (a zombie always needs a collar)";
            }
            Item current = slot == "armor" ? z.Armor : z.Hand;
            if (current == null || current.IsEmpty)
            {
                return "OK " + slot + " was already empty";
            }
            BodyInventory(z).RemoveItemFromInventoryByUID(current);
            SetSlot(z, slot, SGuid.Empty);
            ToolChanged(z);
            return "OK " + slot + " emptied";
        }

        private static void SetSlot(ZombieWgoData z, string slot, SGuid uid)
        {
            if (slot == "collar") z.equippedCollar.SetGuid(uid);
            else if (slot == "armor") z.equippedArmor.SetGuid(uid);
            else z.equippedHand.SetGuid(uid);
        }

        // Same as ZombieEquipmentInventoryWidgetData.TryUpdateZombie: let the worker pick up the new tool.
        private static void ToolChanged(ZombieWgoData z)
        {
            switch (z.ZombieType)
            {
                case ZombieType.Crafter:
                case ZombieType.Worker:
                    z.CrafterOnToolChanged();
                    break;
                case ZombieType.ConveyorCrafter:
                    z.ConveyorCrafterOnToolChanged();
                    break;
                case ZombieType.Gardener:
                    z.GardenerOnToolChanged();
                    break;
                case ZombieType.Free:
                case ZombieType.Caretaker:
                case ZombieType.Porter:
                    break;
                default:
                    z.FighterOnEquipmentChange();
                    break;
            }
        }

        private static string SetPerk(ZombieWgoData z, string id, bool learn)
        {
            TalentLevelUpDef def = GameBalance.Me.GetDataOrNull<TalentLevelUpDef>(id);
            if (def == null || !def.isZombiePerk)
            {
                return "ERR unknown zombie skill '" + id + "'";
            }
            ZombieTalentData branch = z.GetTalentBranch(def.talentId);
            if (branch == null)
            {
                return "ERR this zombie has no " + def.talentId + " branch";
            }
            if (learn)
            {
                if (!branch.studiedLevelUps.Contains(id))
                {
                    z.PurchaseTalentLevelUp(def, free: true);
                    z.CheckRedSkulls(addRedSkulls: false); // more skills than red skulls -> the game disables the extras
                }
                return z.disabledTalentLevelUps.Contains(id)
                    ? "OK learned " + id + " but it is inactive: not enough red skulls (" + z.RedSkulls + ")"
                    : "OK learned " + id;
            }
            if (!branch.studiedLevelUps.Remove(id))
            {
                return "OK " + id + " was not learned";
            }
            if (!z.disabledTalentLevelUps.Remove(id))
            {
                branch.curTalentValue -= def.talentValueAdd; // disabled skills already had this undone
                if (!string.IsNullOrEmpty(def.linkedPerk))
                {
                    z.RemovePerk(def.linkedPerk);
                }
            }
            z.CheckRedSkulls(addRedSkulls: true); // a freed slot re-activates a disabled skill
            return "OK forgot " + id;
        }

        // ---------- JSON out ----------

        private static string Describe(ZombieWgoData z)
        {
            var j = new Json();
            j.Begin();
            j.Str("name", z.Name).Str("type", z.ZombieType.ToString());
            UIZombieWorkerWindowData data = OpenData();
            j.Str("state", data != null ? data.State.ToString() : "");
            j.Int("techRed", z.techRed).Int("techGreen", z.techGreen).Int("techBlue", z.techBlue);
            j.Int("white", z.WhiteSkulls).Int("red", z.RedSkulls);
            int used = z.GetUsedPerksCount();
            j.Int("perksUsed", used).Int("perksDisabled", z.disabledTalentLevelUps.Count);
            Item collar = z.Collar;
            if (collar != null && !collar.IsEmpty)
            {
                j.Int("collarRedMax", collar.Definition.redSkullsMaxCollar).Int("collarRedMin", collar.Definition.redSkullsMinCollar);
                j.Bool("inCollarLimits", collar.Definition.SkullsInBorders(z.WhiteSkulls, z.RedSkulls));
            }
            j.Arr("items");
            foreach (Item it in z.ZombieItem.Inventory)
            {
                if (it == null || it.IsEmpty) continue;
                ItemDef d = it.Definition;
                j.Obj().Str("uid", it.UniqueId.ToString()).Str("id", it.id).Int("count", it.Count)
                    .Int("white", d.whiteSkulls * it.Count).Int("red", d.redSkulls * it.Count)
                    .Str("slot", SlotOf(z, it)).Str("group", OrganGroup(d)).Str("perk", d.bodyLinkedPerk ?? "").End();
            }
            j.EndArr();
            j.Arr("talents");
            foreach (ZombieTalentData t in z.talentData)
            {
                j.Obj().Str("id", t.id).Int("value", t.curTalentValue).StrList("studied", t.studiedLevelUps).End();
            }
            j.EndArr();
            j.StrList("disabled", z.disabledTalentLevelUps);
            j.End();
            return j.ToString();
        }

        // Organ kind (gr_heart, gr_brain, gr_fat ...) - only for real body parts; food, fish and
        // stories also use gr_* groups but don't belong in a body.
        internal static string OrganGroup(ItemDef d)
        {
            if (d.itemGroupIds != null && d.itemGroupIds.Contains("bodypart"))
            {
                foreach (string g in d.itemGroupIds)
                {
                    if (g.StartsWith("gr_")) return g;
                }
            }
            return "";
        }

        // Everything the app needs to offer choices: equipment, skull items and the zombie skill tree.
        private static string Catalog()
        {
            if (GameBalance.Me == null)
            {
                return "ERR game data not loaded yet";
            }
            var j = new Json();
            j.Begin();
            var collars = new List<string>();
            var hands = new List<string>();
            var armors = new List<string>();
            j.Arr("body");
            foreach (ItemDef d in GameBalance.Me.itemDefs)
            {
                if (d == null || string.IsNullOrEmpty(d.id)) continue;
                if (d.type == ItemType.Collar) collars.Add(d.id);
                else if (d.type == ItemType.BodyArmor) armors.Add(d.id);
                else if ((d.isTool || d.isWeapon) && d.type != ItemType.Sword) hands.Add(d.id);
                else if (d.whiteSkulls != 0 || d.redSkulls != 0 || d.type == ItemType.Embalm || OrganGroup(d) != "")
                {
                    j.Obj().Str("id", d.id).Int("white", d.whiteSkulls).Int("red", d.redSkulls).Str("group", OrganGroup(d)).Str("perk", d.bodyLinkedPerk ?? "").End();
                }
            }
            j.EndArr();
            j.StrList("collars", collars).StrList("hands", hands).StrList("armors", armors);
            j.Arr("collarLimits");
            foreach (string c in collars)
            {
                ItemDef d = Def(c);
                j.Obj().Str("id", c).Int("redMax", d.redSkullsMaxCollar).End();
            }
            j.EndArr();
            j.Arr("skills");
            foreach (TalentLevelUpDef t in GameBalance.Me.talentLevelUpDefs)
            {
                if (t == null || !t.isZombiePerk) continue;
                string name = !string.IsNullOrEmpty(t.linkedPerk) ? Loc(t.linkedPerk) : "";
                j.Obj().Str("id", t.id).Str("branch", t.talentId).Str("name", name).Str("perk", t.linkedPerk ?? "")
                    .Int("value", t.talentValueAdd).Int("costRed", t.techRed).Int("costGreen", t.techGreen).Int("costBlue", t.techBlue)
                    .StrList("parents", t.parents).End();
            }
            j.EndArr();
            j.Arr("branches");
            foreach (TalentDef t in GameBalance.Me.talentDefs)
            {
                if (t == null) continue;
                j.Obj().Str("id", t.id).Str("name", Loc(t.id)).End();
            }
            j.EndArr();
            j.End();
            return "OK " + j.ToString();
        }

        private static string Loc(string key)
        {
            try
            {
                string s = LLBase.L(key);
                return string.IsNullOrEmpty(s) || s == key ? "" : s;
            }
            catch (Exception)
            {
                return "";
            }
        }

        // Tiny one-line JSON writer (no JSON library ships with the game's .NET profile).
        internal sealed class Json
        {
            private readonly StringBuilder sb = new StringBuilder();
            private bool first = true;

            private void Sep() { if (!first) sb.Append(','); first = false; }
            private void Key(string k) { Sep(); Esc(k); sb.Append(':'); }

            private void Esc(string s)
            {
                sb.Append('"');
                foreach (char c in s ?? "")
                {
                    switch (c)
                    {
                        case '"': sb.Append("\\\""); break;
                        case '\\': sb.Append("\\\\"); break;
                        case '\n': sb.Append("\\n"); break;
                        case '\r': sb.Append("\\r"); break;
                        case '\t': sb.Append("\\t"); break;
                        default:
                            if (c < 0x20) sb.Append("\\u").Append(((int)c).ToString("x4"));
                            else sb.Append(c);
                            break;
                    }
                }
                sb.Append('"');
            }

            public Json Begin() { sb.Append('{'); first = true; return this; }
            public Json End() { sb.Append('}'); first = false; return this; }
            public Json Obj() { Sep(); sb.Append('{'); first = true; return this; }
            public Json Arr(string k) { Key(k); sb.Append('['); first = true; return this; }
            public Json EndArr() { sb.Append(']'); first = false; return this; }
            public Json Str(string k, string v) { Key(k); Esc(v); return this; }
            public Json Int(string k, int v) { Key(k); sb.Append(v.ToString(CultureInfo.InvariantCulture)); return this; }
            public Json Bool(string k, bool v) { Key(k); sb.Append(v ? "true" : "false"); return this; }

            public Json StrList(string k, IEnumerable<string> list)
            {
                Key(k);
                sb.Append('[');
                bool f = true;
                if (list != null)
                {
                    foreach (string s in list)
                    {
                        if (!f) sb.Append(',');
                        Esc(s);
                        f = false;
                    }
                }
                sb.Append(']');
                return this;
            }

            public override string ToString() { return sb.ToString(); }
        }
    }
}
