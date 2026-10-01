// Time fast-forward: runs the whole world faster (the game's own sleep mechanism,
// UpdateManager.SetTimeSpeedMultiplier) until a chosen day and hour, then back to normal.
// Nothing is skipped: clock, crafts, gardens, zombies, quests all simulate at the higher speed,
// in the game's normal fixed steps. The game itself uses x50 while the player sleeps.
//
//   TIME GET                          -> OK day=153 weekday=3 tod=0.5812 len=5 speed=1 ff=-
//   TIME FF <weekday 1-6|+N> <hour 0-23> <speed 2-50> <rested 1|0>
//        weekday: 1 Gluttony 2 Sloth 3 Lust 4 Envy 5 Pride 6 Wrath (next time that day starts) ; +N = N days ahead
//   TIME STOP
//
// Runs on Unity's main thread (Bridge.Pump calls Tick every frame).

using System;
using System.Globalization;
using UnityEngine;

namespace GK2Spawner
{
    public static class TimeFF
    {
        private const float MaxSpeed = 50f; // the game's own sleep speed
        private static bool active;
        private static int targetDay;
        private static float targetTod;
        private static float speed = 1f;
        private static bool keepRested;
        private static int lastDaySeen;

        private static readonly string[] DayNames = { "", "Gluttony", "Sloth", "Lust", "Envy", "Pride", "Wrath" };

        private static bool Ready(out EnvironmentData env, out EnvironmentEngine eng)
        {
            env = null;
            eng = null;
            if (MainGame.Instance == null || MainGame.Instance.GameSave == null || MainGame.PlayerData == null) return false;
            env = MainGame.Instance.GameSave.environmentData;
            eng = EnvironmentEngine.Instance;
            return env != null && eng != null && MainGame.UpdateManager != null;
        }

        public static string Handle(string[] args)
        {
            EnvironmentData env;
            EnvironmentEngine eng;
            if (!Ready(out env, out eng)) return "ERR no save loaded - load your game first";
            string sub = args.Length > 0 ? args[0].ToUpperInvariant() : "";
            switch (sub)
            {
                case "GET":
                    return "OK " + Describe(env, eng);
                case "STOP":
                    Stop(env);
                    return "OK fast-forward stopped - " + Describe(env, eng);
                case "FF":
                    return Start(args, env, eng);
                default:
                    return "ERR usage: TIME GET | TIME FF <weekday 1-6|+N> <hour 0-23> <speed 2-50> <rested 1|0> | TIME STOP";
            }
        }

        private static string Describe(EnvironmentData env, EnvironmentEngine eng)
        {
            var c = CultureInfo.InvariantCulture;
            return "day=" + env.Day + " weekday=" + env.CurrentDayNumber + " tod=" + eng.timeOfDay.ToString("0.0000", c)
                + " len=" + eng.gameplayDayInMinutes.ToString("0.##", c) + " speed=" + MainGame.UpdateManager.TimeMultiplier.ToString("0.##", c)
                + " ff=" + (active ? targetDay + "@" + targetTod.ToString("0.0000", c) : "-");
        }

        private static string Start(string[] args, EnvironmentData env, EnvironmentEngine eng)
        {
            int hour, rested;
            float spd;
            if (args.Length != 5 || !int.TryParse(args[2], out hour) || hour < 0 || hour > 23
                || !float.TryParse(args[3], NumberStyles.Float, CultureInfo.InvariantCulture, out spd) || spd < 2f || spd > MaxSpeed
                || !int.TryParse(args[4], out rested))
            {
                return "ERR usage: TIME FF <weekday 1-6|+N> <hour 0-23> <speed 2-50> <rested 1|0>";
            }
            float tod = hour / 24f;
            float now = eng.timeOfDay;
            int day = env.Day;
            int target;
            if (args[1].StartsWith("+"))
            {
                int n;
                if (!int.TryParse(args[1].Substring(1), out n) || n < 1 || n > 60) return "ERR days ahead must be +1 to +60";
                target = day + n;
            }
            else
            {
                int wd;
                if (!int.TryParse(args[1], out wd) || wd < 1 || wd > 6) return "ERR weekday must be 1-6";
                target = day;
                // next moment that is (weekday wd, hour) and lies in the future
                while (env.GetDayNumberFromDay(target) != wd || (target == day && tod <= now)) target++;
            }
            if (MainGame.PlayerData.energySystem.IsSleeping || MainGame.PlayerData.energySystem.IsInTransitionBetweenSleep)
            {
                return "ERR you are sleeping - wake up first";
            }
            Cheats.Freeze = false; // a frozen clock would never reach the target
            active = true;
            targetDay = target;
            targetTod = tod;
            speed = spd;
            keepRested = rested == 1;
            lastDaySeen = day;
            MainGame.UpdateManager.SetTimeSpeedMultiplier(speed);
            float daysLeft = (target - day) + (tod - now);
            float realSeconds = daysLeft * eng.gameplayDayInMinutes * 60f / speed;
            return "OK fast-forwarding x" + speed.ToString("0", CultureInfo.InvariantCulture) + " to day " + target + " (" + DayNames[env.GetDayNumberFromDay(target)] + ") "
                + hour.ToString("00") + ":00 - about " + Mathf.CeilToInt(realSeconds) + " s real time";
        }

        private static void Stop(EnvironmentData env)
        {
            if (!active) return;
            active = false;
            EnergySystem es = MainGame.PlayerData != null ? MainGame.PlayerData.energySystem : null;
            // don't cut the game's own sleep speed-up short
            if (es == null || (!es.IsSleeping && !es.IsInTransitionBetweenSleep))
            {
                MainGame.UpdateManager.SetTimeSpeedMultiplier(1f);
            }
            if (keepRested && es != null) es.DeactivateLackOfSleep();
        }

        // Called every frame from Bridge.Pump.
        public static void Tick()
        {
            if (!active) return;
            EnvironmentData env;
            EnvironmentEngine eng;
            if (!Ready(out env, out eng))
            {
                active = false; // save unloaded
                return;
            }
            if (env.Day > targetDay || (env.Day == targetDay && eng.timeOfDay >= targetTod))
            {
                Stop(env);
                return;
            }
            EnergySystem es = MainGame.PlayerData.energySystem;
            if (keepRested && env.Day != lastDaySeen)
            {
                lastDaySeen = env.Day;
                es.DeactivateLackOfSleep();
            }
            // Sleeping switches the game to x50 and back to x1 on waking; re-apply ours afterwards.
            if (!es.IsSleeping && !es.IsInTransitionBetweenSleep && !Mathf.Approximately(MainGame.UpdateManager.TimeMultiplier, speed))
            {
                MainGame.UpdateManager.SetTimeSpeedMultiplier(speed);
            }
        }
    }
}
