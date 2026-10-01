// Common trainer options. Each switch is applied every frame from Bridge.Pump while it's on,
// using the game's own systems (no memory patching):
//   god       PlayerData.hpComponent.IsImmuneToDamage + RestoreFullHp
//   stamina   stamina resource kept at its max      (PlayerStaminaGameResSystem)
//   energy    energy resource kept at its max       (max is 100 - insanity)
//   insanity  insanity resource kept at its min (0)
//   sleep     lack-of-sleep debuff cleared          (EnergySystem.DeactivateLackOfSleep)
//   gather    a tree / rock / stump / ripe plant you hit dies after the first hit (if you have the mastery)
//   freeze    world clock stopped                   (EnvironmentEngine.IsPaused)
//   move      player speed multiplier               (PlayerPhysicsConfig.speed, restored when set back to 1)
//   game      game speed                            (Time.timeScale; the game's own pause (0) is left alone)
//
//   CHEAT GET                     -> OK god=0 stamina=1 ... move=1.5 game=1
//   CHEAT SET <switch> 1|0
//   CHEAT SPEED move|game <x>
//   CHEAT RESTORE                 -> full health, energy and stamina, zero insanity, once

using System;
using System.Collections.Generic;
using System.Globalization;
using LazyBearTechnology;
using UnityEngine;

namespace GK2Spawner
{
    public static class Cheats
    {
        private static bool god, stamina, energy, insanity, sleep, gather, freeze;
        private static float moveMul = 1f, gameSpeed = 1f;
        private static PlayerPhysicsConfig speedConfig;
        private static float baseSpeed;
        private static string lastError;

        public static bool Freeze
        {
            get { return freeze; }
            set
            {
                freeze = value;
                if (!value && EnvironmentEngine.Instance != null) EnvironmentEngine.Instance.IsPaused = false;
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
                        default: return "ERR unknown switch '" + args[1] + "'";
                    }
                    return "OK " + args[1].ToLowerInvariant() + (on ? " on" : " off");
                }
                case "SPEED":
                {
                    float x;
                    if (args.Length != 3 || !float.TryParse(args[2], NumberStyles.Float, CultureInfo.InvariantCulture, out x)) return "ERR usage: CHEAT SPEED move|game <x>";
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
                    return "ERR usage: CHEAT SPEED move|game <x>";
                }
                case "RESTORE":
                    MainGame.PlayerData.hpComponent.RestoreFullHp();
                    SetMax("energy");
                    SetMax("stamina");
                    SetMin("insanity");
                    return "OK health, energy and stamina full, insanity 0";
                default:
                    return "ERR usage: CHEAT GET | SET <switch> 1|0 | SPEED move|game <x> | RESTORE";
            }
        }

        private static string State()
        {
            var c = CultureInfo.InvariantCulture;
            return "god=" + B(god) + " stamina=" + B(stamina) + " energy=" + B(energy) + " insanity=" + B(insanity)
                + " sleep=" + B(sleep) + " gather=" + B(gather) + " freeze=" + B(freeze)
                + " move=" + moveMul.ToString("0.##", c) + " game=" + gameSpeed.ToString("0.##", c);
        }

        private static string B(bool b) { return b ? "1" : "0"; }

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
                if (god)
                {
                    pd.hpComponent.IsImmuneToDamage = true;
                    if (pd.hpComponent.Hp < pd.hpComponent.MaxHpValue) pd.hpComponent.RestoreFullHp();
                }
                if (stamina) SetMax("stamina");
                if (insanity) SetMin("insanity");
                if (energy) SetMax("energy");
                if (sleep) pd.energySystem.DeactivateLackOfSleep();
                if (freeze && EnvironmentEngine.Instance != null && !EnvironmentEngine.Instance.IsPaused) EnvironmentEngine.Instance.IsPaused = true;
                if (moveMul != 1f || speedConfig != null) ApplyMoveSpeed();
                if (gameSpeed != 1f && Time.timeScale > 0f && Mathf.Abs(Time.timeScale - gameSpeed) > 0.01f) Time.timeScale = gameSpeed;
                if (gather) OneHitGather();
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
    }
}
