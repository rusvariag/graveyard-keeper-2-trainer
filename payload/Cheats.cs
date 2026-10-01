// Common trainer options. Each switch is applied every frame from Bridge.Pump while it's on,
// using the game's own systems (no memory patching):
//   god       PlayerData.hpComponent.IsImmuneToDamage + RestoreFullHp
//   stamina   stamina resource kept at its max      (PlayerStaminaGameResSystem)
//   energy    energy resource kept at its max       (max is 100 - insanity)
//   insanity  insanity resource kept at its min (0)
//   sleep     lack-of-sleep debuff cleared          (EnergySystem.DeactivateLackOfSleep)
//   gather    a tree / rock / stump / ripe plant you hit dies after the first hit (if you have the mastery)
//   freeze    world clock stopped                   (EnvironmentEngine.IsPaused)
//   noon      clock held at 12:00                   (EnvironmentEngine.SetTimeOfDay + IsPaused)
//   move      player speed multiplier               (PlayerPhysicsConfig.speed, restored when set back to 1)
//   game      game speed                            (Time.timeScale; the game's own pause (0) is left alone)
//   tech      tech points earned x1-10              (every gain of tech_red/green/blue is scaled)
//   friend    NPC friendship earned x1-10           (every gain of an NPC's *_REP is scaled, up to 100)
//   energy    energy used x0-2                      (every drop of the energy resource is scaled)
//   stamina   stamina used x0-2                     (every drop of the stamina resource is scaled)
// The multipliers compare each value with the previous frame; Resync() is called after every trainer
// command, so the trainer's own changes (TECH, NPC SET, ...) are never scaled.
//
//   CHEAT GET                     -> OK god=0 stamina=1 ... move=1.5 game=1 tech=1 friend=1 energyUse=1 staminaUse=1
//   CHEAT SET <switch> 1|0
//   CHEAT SPEED move|game|tech|friend|energy|stamina <x>
//   CHEAT RESTORE                 -> full health, energy and stamina, zero insanity, once
//   CHEAT HAPPY [value]           -> OK happiness=12 town=40 (town gratitude; value 0-9999 sets it)
//   NPC GET                       -> OK [{"id":"nun_REP","name":"Nun","value":35}, ...]
//   NPC SET <repId> <value>       (PlayerData.SetNPCRep, which also re-checks reputation unlocks)
//   NPC MAX                       -> every NPC to 100

using System;
using System.Collections.Generic;
using System.Globalization;
using LazyBearTechnology;
using UnityEngine;

namespace GK2Spawner
{
    public static class Cheats
    {
        private static bool god, stamina, energy, insanity, sleep, gather, freeze, noon;
        private static float moveMul = 1f, gameSpeed = 1f;
        private static float techMul = 1f, repMul = 1f, energyUse = 1f, staminaUse = 1f;
        private static readonly string[] TechRes = { "tech_red", "tech_green", "tech_blue" };
        private static readonly Dictionary<string, float> lastValue = new Dictionary<string, float>();
        private static PlayerData trackedPlayer;
        private static PlayerPhysicsConfig speedConfig;
        private static float baseSpeed;
        private static string lastError;

        public static bool Freeze
        {
            get { return freeze; }
            set
            {
                freeze = value;
                if (!value && !noon && EnvironmentEngine.Instance != null) EnvironmentEngine.Instance.IsPaused = false;
            }
        }

        public static bool Noon
        {
            get { return noon; }
            set
            {
                noon = value;
                if (!value && !freeze && EnvironmentEngine.Instance != null) EnvironmentEngine.Instance.IsPaused = false;
            }
        }

        private static bool InGame()
        {
            return MainGame.Instance != null && MainGame.Instance.GameSave != null && MainGame.PlayerData != null && MainGame.PlayerController != null;
        }

        public static string Handle(string[] args)
        {
            if (!InGame()) return "ERR no save loaded - load your game first";
            string sub = args.Length > 0 ? args[0].ToUpperInvariant() : "";
            switch (sub)
            {
                case "GET":
                    return "OK " + State();
                case "SET":
                {
                    if (args.Length != 3 || (args[2] != "1" && args[2] != "0")) return "ERR usage: CHEAT SET <switch> 1|0";
                    bool on = args[2] == "1";
                    switch (args[1].ToLowerInvariant())
                    {
                        case "god": god = on; if (!on) MainGame.PlayerData.hpComponent.IsImmuneToDamage = false; break;
                        case "stamina": stamina = on; break;
                        case "energy": energy = on; break;
                        case "insanity": insanity = on; break;
                        case "sleep": sleep = on; break;
                        case "gather": gather = on; break;
                        case "freeze": Freeze = on; break;
                        case "noon": Noon = on; break;
                        default: return "ERR unknown switch '" + args[1] + "'";
                    }
                    return "OK " + args[1].ToLowerInvariant() + (on ? " on" : " off");
                }
                case "SPEED":
                {
                    float x;
                    if (args.Length != 3 || !float.TryParse(args[2], NumberStyles.Float, CultureInfo.InvariantCulture, out x)) return "ERR usage: CHEAT SPEED move|game|tech|friend|energy|stamina <x>";
                    string which = args[1].ToLowerInvariant();
                    if (which == "move")
                    {
                        if (x < 1f || x > 3f) return "ERR move speed must be 1-3";
                        moveMul = x;
                        ApplyMoveSpeed();
                        return "OK move speed x" + x.ToString("0.##", CultureInfo.InvariantCulture);
                    }
                    if (which == "game")
                    {
                        if (x < 0.25f || x > 4f) return "ERR game speed must be 0.25-4";
                        gameSpeed = x;
                        if (Time.timeScale > 0f) Time.timeScale = x;
                        return "OK game speed x" + x.ToString("0.##", CultureInfo.InvariantCulture);
                    }
                    if (which == "tech" || which == "friend")
                    {
                        if (x < 1f || x > 10f) return "ERR multiplier must be 1-10";
                        Resync();
                        if (which == "tech") techMul = x; else repMul = x;
                        return "OK " + (which == "tech" ? "tech points" : "NPC friendship") + " gains x" + x.ToString("0.##", CultureInfo.InvariantCulture);
                    }
                    if (which == "energy" || which == "stamina")
                    {
                        if (x < 0f || x > 2f) return "ERR use rate must be 0-2";
                        Resync();
                        if (which == "energy") energyUse = x; else staminaUse = x;
                        return "OK " + which + " use x" + x.ToString("0.##", CultureInfo.InvariantCulture);
                    }
                    return "ERR usage: CHEAT SPEED move|game|tech|friend|energy|stamina <x>";
                }
                case "RESTORE":
                    MainGame.PlayerData.hpComponent.RestoreFullHp();
                    SetMax("energy");
                    SetMax("stamina");
                    SetMin("insanity");
                    return "OK health, energy and stamina full, insanity 0";
                case "HAPPY":
                {
                    int v;
                    if (args.Length == 2)
                    {
                        if (!int.TryParse(args[1], out v) || v < 0 || v > HappinessMax) return "ERR happiness must be 0-" + HappinessMax;
                        MainGame.PlayerData.SetRes("happiness", v); // raises OnGameResChanged so the HUD updates
                    }
                    else if (args.Length != 1)
                    {
                        return "ERR usage: CHEAT HAPPY [0-" + HappinessMax + "]";
                    }
                    return "OK happiness=" + MainGame.PlayerData.GetResInt("happiness") + " town=" + MainGame.Instance.GameSave.townSystem.Quality;
                }
                default:
                    return "ERR usage: CHEAT GET | SET <switch> 1|0 | SPEED <name> <x> | RESTORE | HAPPY [value]";
            }
        }

        private static string State()
        {
            var c = CultureInfo.InvariantCulture;
            return "god=" + B(god) + " stamina=" + B(stamina) + " energy=" + B(energy) + " insanity=" + B(insanity)
                + " sleep=" + B(sleep) + " gather=" + B(gather) + " freeze=" + B(freeze) + " noon=" + B(noon)
                + " move=" + moveMul.ToString("0.##", c) + " game=" + gameSpeed.ToString("0.##", c)
                + " tech=" + techMul.ToString("0.##", c) + " friend=" + repMul.ToString("0.##", c)
                + " energyUse=" + energyUse.ToString("0.##", c) + " staminaUse=" + staminaUse.ToString("0.##", c);
        }

        private static string B(bool b) { return b ? "1" : "0"; }

        private const int HappinessMax = 9999; // GameResSystemDef "happiness" max

        private static void SetMax(string res)
        {
            GK2GameResSystem s = GK2GameResSystem.GetSystem(res);
            if (s != null && !s.HasMax()) s.Set(s.Max);
        }

        private static void SetMin(string res)
        {
            GK2GameResSystem s = GK2GameResSystem.GetSystem(res);
            if (s != null && s.IsEnoughValue(s.Min + 0.5f)) s.Set(s.Min);
        }

        private static void ApplyMoveSpeed()
        {
            PlayerPhysicsConfig cfg = MainGame.PlayerController.PhysicalBody != null ? MainGame.PlayerController.PhysicalBody.PhysicsConfig : null;
            if (cfg == null) return;
            if (cfg != speedConfig)
            {
                speedConfig = cfg;      // new config object: its speed is the unmodified base
                baseSpeed = cfg.speed;
            }
            float want = baseSpeed * moveMul;
            if (!Mathf.Approximately(cfg.speed, want)) cfg.speed = want;
        }

        // Called every frame from Bridge.Pump.
        public static void Tick()
        {
            if (!InGame()) return;
            try
            {
                PlayerData pd = MainGame.PlayerData;
                ScaleChanges(pd);
                if (god)
                {
                    pd.hpComponent.IsImmuneToDamage = true;
                    if (pd.hpComponent.Hp < pd.hpComponent.MaxHpValue) pd.hpComponent.RestoreFullHp();
                }
                if (stamina) SetMax("stamina");
                if (insanity) SetMin("insanity");
                if (energy) SetMax("energy");
                if (sleep) pd.energySystem.DeactivateLackOfSleep();
                if (noon && EnvironmentEngine.Instance != null)
                {
                    EnvironmentEngine eng = EnvironmentEngine.Instance;
                    if (Mathf.Abs(eng.timeOfDay - 0.5f) > 0.0005f) eng.SetTimeOfDay(0.5f);
                    if (!eng.IsPaused) eng.IsPaused = true;
                }
                if (freeze && EnvironmentEngine.Instance != null && !EnvironmentEngine.Instance.IsPaused) EnvironmentEngine.Instance.IsPaused = true;
                if (moveMul != 1f || speedConfig != null) ApplyMoveSpeed();
                if (gameSpeed != 1f && Time.timeScale > 0f && Mathf.Abs(Time.timeScale - gameSpeed) > 0.01f) Time.timeScale = gameSpeed;
                if (gather) OneHitGather();
                Resync(); // the switches above changed values: don't scale those next frame
                lastError = null;
            }
            catch (Exception e)
            {
                if (lastError != e.Message)
                {
                    lastError = e.Message;
                    Debug.LogWarning("[GK2Spawner] cheats: " + e);
                }
            }
        }

        // ---------- multipliers ----------

        // Remembers the current values, so the next frame only sees changes made by the game.
        public static void Resync()
        {
            PlayerData pd = MainGame.PlayerData;
            if (pd == null) return;
            trackedPlayer = pd;
            foreach (string id in TechRes) lastValue[id] = pd.GetRes(id);
            foreach (string[] n in Npcs) lastValue[n[0]] = pd.GetNPCRep(n[0]);
            lastValue["energy"] = pd.GetRes("energy");
            lastValue["stamina"] = pd.GetRes("stamina");
        }

        private static void ScaleChanges(PlayerData pd)
        {
            if (pd != trackedPlayer)
            {
                Resync(); // another save was loaded
                return;
            }
            float before;
            foreach (string id in TechRes)
            {
                float now = pd.GetRes(id);
                if (techMul != 1f && lastValue.TryGetValue(id, out before) && now > before + 0.001f)
                {
                    pd.SetRes(id, before + (now - before) * techMul); // clamped to 999 by the game
                }
            }
            foreach (string[] n in Npcs)
            {
                int now = pd.GetNPCRep(n[0]);
                if (repMul != 1f && lastValue.TryGetValue(n[0], out before) && now > before + 0.5f && now < 100)
                {
                    int want = Mathf.Min(100, Mathf.RoundToInt(before + (now - before) * repMul));
                    if (want > now) pd.SetNPCRep(n[0], want);
                }
            }
            ScaleUse(pd, "energy", energyUse);
            ScaleUse(pd, "stamina", staminaUse);
        }

        // A drop of `res` since the last frame becomes drop * rate (energy/stamina use).
        private static void ScaleUse(PlayerData pd, string res, float rate)
        {
            float before;
            if (rate == 1f || !lastValue.TryGetValue(res, out before)) return;
            float now = pd.GetRes(res);
            if (now < before - 0.001f)
            {
                pd.SetRes(res, before - (before - now) * rate); // clamped to the resource's min/max by the game
            }
        }

        // After your first hit on a tree / rock / stump / ripe plant, finish it - like a max-damage hit.
        private static void OneHitGather()
        {
            PlayerHPActivity act = MainGame.PlayerController.WorkerActivity as PlayerHPActivity;
            if (act == null || act.WgoData == null) return;
            HPComponent hp = act.WgoData.HpComponent;
            if (hp == null || hp.Hp <= 0 || !hp.WasDamagedAtLeastOnce) return;
            if (act.WgoData.Definition.playerHpActivityMod <= 0) return; // repair-type objects
            if (!act.IsEnoughMastery()) return;                          // the game wouldn't let this hit count either
            hp.ApplyDamage(hp.Hp);
        }

        // ---------- NPC friendship ----------

        private static readonly string[][] Npcs =
        {
            new[] { "nun_REP", "Agatha (Nun)" },
            new[] { "npc_head_of_the_guards_REP", "Herbert" },
            new[] { "npc_plague_doctor_REP", "Albert" },
            new[] { "npc_tavern_owner_REP", "Linda" },
            new[] { "npc_workshop_foreman_REP", "Jack" },
            new[] { "npc_astrologer_REP", "Gunter" },
            new[] { "npc_larry_REP", "Larry" },
            new[] { "village_REP", "Village" },
        };

        public static string HandleNpc(string[] args)
        {
            if (!InGame()) return "ERR no save loaded - load your game first";
            PlayerData pd = MainGame.PlayerData;
            string sub = args.Length > 0 ? args[0].ToUpperInvariant() : "";
            if (sub == "GET")
            {
                var parts = new List<string>();
                foreach (string[] n in Npcs)
                {
                    parts.Add("{\"id\":\"" + n[0] + "\",\"name\":\"" + n[1] + "\",\"value\":" + pd.GetNPCRep(n[0]).ToString(CultureInfo.InvariantCulture) + "}");
                }
                return "OK [" + string.Join(",", parts.ToArray()) + "]";
            }
            if (sub == "SET")
            {
                int v;
                if (args.Length != 3 || !int.TryParse(args[2], out v) || v < 0 || v > 100) return "ERR usage: NPC SET <repId> <0-100>";
                if (Array.Find(Npcs, n => n[0] == args[1]) == null) return "ERR unknown NPC '" + args[1] + "'";
                pd.SetNPCRep(args[1], v);
                return "OK " + Array.Find(Npcs, n => n[0] == args[1])[1] + " friendship " + v;
            }
            if (sub == "MAX")
            {
                foreach (string[] n in Npcs) pd.SetNPCRep(n[0], 100);
                return "OK every NPC's friendship set to 100";
            }
            return "ERR usage: NPC GET | SET <repId> <0-100> | MAX";
        }
    }
}
