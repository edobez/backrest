# Operations Guide

This guide details the core operations available in Backrest and how to configure them effectively. "Operations" in Backrest refer to any task that interacts with your repository, such as creating backups, managing retention, verifying integrity, or restoring files.

## Restic Integration

Backrest executes operations through the [restic](https://restic.net) backup tool. Each operation maps to specific restic commands with additional functionality provided by Backrest.

### Binary Management
- **Location**: Backrest searches for the restic binary in the following order:
  1. Data directory (typically `~/.local/share/backrest`)
  2. `/bin/` directory
  3. The system `$PATH`
- **Version Requirement**: Backrest is only tested against the latest version of restic. It will selectively reject outdated versions.
- **Auto-download**: If no valid binary is found, Backrest downloads a verified version from [GitHub releases](https://github.com/restic/restic/releases).
- **Verification**: Downloads are verified using SHA256 checksums signed by restic maintainers.
- **Override**: Set `BACKREST_RESTIC_COMMAND` environment variable to use a custom restic binary.

### Command Execution
- **Environment**: Repository-specific environment variables are injected
- **Flags**: Repository-configured flags are appended to commands
- **Logging**: 
  - Error logs: Last ~500 bytes (split between start/end if longer)
  - Full logs: Available via **[View Logs]** in the UI, truncated to 32KB (split if longer)

::: info
If an operation fails, you can always find the full diagnostic logs in the Backrest UI by clicking on the specific operation block in the history tree. 
:::

## Scheduling System

Backrest provides flexible scheduling options for all operations through policies and clocks.

### Schedule Policies

| Policy         | Description                     | Use Case                                                  |
| -------------- | ------------------------------- | --------------------------------------------------------- |
| Disabled       | Operation will not run          | Temporarily disable operations                            |
| Cron           | Standard cron expression timing | Precise scheduling (e.g., `0 0 * * *` for daily midnight) |
| Interval Days  | Run every N days                | Regular daily+ intervals                                  |
| Interval Hours | Run every N hours               | Regular sub-daily intervals                               |

### Cron Expression Syntax

Cron schedules accept the standard five fields (minute, hour, day of month, month, day of week) with the usual `*`, `,`, `-`, and `/` operators. Day of week accepts `0`-`7` (both `0` and `7` mean Sunday) or the names `SUN`-`SAT`.

The following extensions are also supported:

| Syntax      | Meaning                                    | Example                                           |
| ----------- | ------------------------------------------ | ------------------------------------------------- |
| `#`         | Nth weekday of the month                   | `0 0 * * 1#1` runs on the first Monday             |
| `L`         | Last day of the month                      | `0 0 L * *` runs on the last day of each month     |
| `<dow>L`    | Last given weekday of the month            | `0 0 * * 5L` runs on the last Friday               |
| `<dom>W`    | Nearest weekday to a day of the month      | `0 0 15W * *` runs on the weekday nearest the 15th |
| `?`         | Same as `*`                                | `0 0 * * ?`                                        |
| `@` aliases | `@yearly`, `@monthly`, `@weekly`, `@daily`, `@hourly` | `@daily` runs at midnight              |

Longer expressions are also accepted. Six fields adds a trailing year (`minute hour day-of-month month day-of-week year`), and seven fields adds both a leading seconds field and a trailing year (`second minute hour day-of-month month day-of-week year`).

Expressions that are syntactically valid but can never match a real date, such as `0 0 30 2 *`, are rejected when the configuration is saved rather than silently never running.

### Schedule Clocks

| Clock         | Description                    | Best For                                |
| ------------- | ------------------------------ | --------------------------------------- |
| Local         | Local timezone wall-clock      | Frequent operations (hourly+)           |
| UTC           | UTC timezone wall-clock        | Cross-timezone coordination             |
| Last Run Time | Relative to previous execution | Infrequent operations, preventing skips |

::: info
**Scheduling Best Practices**
- **Backup Operations** (Plan Settings):
  - Hourly or more frequent: Use "Local" clock
  - Daily or less frequent: Use "Last Run Time" clock
- **Prune/Check Operations** (Repo Settings):
  - Run infrequently (e.g., monthly)
  - Use "Last Run Time" clock to prevent skips
- **Index Snapshots** (Repo Settings):
  - Hourly or daily, depending on how often other clients back up
:::

## Operation Types

### 💾 Backup
[Restic Documentation](https://restic.readthedocs.io/en/latest/040_backup.html)

Creates snapshots of your data using `restic backup`.

**Process Flow:**
1. **Start**
   - Triggers `CONDITION_SNAPSHOT_START` hooks
   - Applies hook failure policies if needed
2. **Execution**
   - Runs `restic backup`
   - Tags snapshot with `plan:{PLAN_ID}` and `created-by:{INSTANCE_ID}`
3. **Completion**
   - Records operation metadata (files, bytes, snapshot ID)
   - Triggers appropriate hooks:
     - Error: `CONDITION_SNAPSHOT_ERROR`
     - Success: `CONDITION_SNAPSHOT_SUCCESS`
     - In both cases: `CONDITION_SNAPSHOT_END`
4. **Post-processing**
   - Runs forget operation if retention policy exists

**Snapshot Tags:**
- `plan:{PLAN_ID}`: Groups snapshots by backup plan
- `created-by:{INSTANCE_ID}`: Identifies creating Backrest instance

### 🕰️ Forget
[Restic Documentation](https://restic.readthedocs.io/en/latest/060_forget.html)

Manages snapshot retention using `restic forget --tag plan:{PLAN_ID}`.

**Retention Policies:**
- **By Count**: `--keep-last {COUNT}`
- **By Time Period**: `--keep-{hourly,daily,weekly,monthly,yearly} {COUNT}`

#### Repo-level Forget Policy

By default, retention is configured per plan and applied right after each backup, scoped to that plan's snapshots (`restic forget --tag plan:{PLAN_ID},created-by:{INSTANCE_ID} --group-by ''`).

Alternatively, a **Forget Policy** can be enabled in the repository settings. It runs `restic forget` on its own schedule with a single retention policy, and it applies to **every snapshot in the repository**: snapshots from all plans, all Backrest instances, and snapshots created outside Backrest (e.g. by your own scripts calling `restic backup`). When it is enabled, per-plan retention is skipped.

Retention is applied separately within each **group** of snapshots. The **Group Snapshots By** option decides how groups are formed and maps directly to restic's [`--group-by`](https://restic.readthedocs.io/en/latest/060_forget.html#removing-snapshots-according-to-a-policy) flag:

| Group By | restic flag | One group per… | Use when |
|---|---|---|---|
| **Tags** (default) | `--group-by tags` | unique set of tags | Several Backrest plans share the repo and each should keep its own history. |
| **None** | `--group-by ''` | — (whole repo is one group) | You want "keep the last N" across the whole repo, e.g. external snapshots carry a unique tag each. |
| **Host** | `--group-by host` | host | Several machines share the repo; retention per machine, ignoring tags and paths. |
| **Host + Paths** | `--group-by host,paths` | host and backed-up paths | Matches restic's own default; useful for untagged snapshots of different directories. |
| **Host + Tags** | `--group-by host,tags` | host and tag set | Several machines run plans with the same tags into one repo. |

::: warning
With **None**, the retention policy counts every snapshot in the repository, whatever plan, host or tool created it. A `Keep last 7` policy keeps the 7 newest snapshots in the repo and forgets everything else.
:::

**Example 1 — Tags (default): plans sharing a repo**

Two Backrest plans back up into the same repo, with a Forget Policy of `Keep last 3`:

```
ID        Time                 Host    Tags                          Paths
--------------------------------------------------------------------------------
a1000001  2026-06-01 00:00:00  laptop  plan:docs,created-by:laptop    /home/me/docs
b2000001  2026-06-01 01:00:00  laptop  plan:photos,created-by:laptop  /home/me/photos
a1000002  2026-06-02 00:00:00  laptop  plan:docs,created-by:laptop    /home/me/docs
b2000002  2026-06-02 01:00:00  laptop  plan:photos,created-by:laptop  /home/me/photos
...       (5 snapshots of each plan)
```

`plan:docs` and `plan:photos` are separate groups, so each plan keeps its 3 newest snapshots: 6 kept and 4 forgotten.

**Example 2 — None: snapshots created by an external script**

A script backs up ZFS snapshots and tags each restic snapshot with the name of its ZFS snapshot, so every tag is unique:

```
ID        Time                 Host            Tags                                   Paths
---------------------------------------------------------------------------------------------------
62c32030  2026-06-01 00:05:02  assault-pillow  challenger/data@auto-2026-06-01_00-00  /mnt/challenger/data
3b1027ba  2026-06-02 00:05:25  assault-pillow  challenger/data@auto-2026-06-02_00-00  /mnt/challenger/data
ad905638  2026-06-03 00:05:24  assault-pillow  challenger/data@auto-2026-06-03_00-00  /mnt/challenger/data
...       (one snapshot per day)
```

With the default **Tags** grouping, each snapshot is its own group, so `Keep last 7` keeps all of them. Set **Group Snapshots By** to **None** and the whole repo becomes one group: only the 7 newest snapshots are kept. **Host + Paths** gives the same result here, because all the snapshots share the same host and path.

**Example 3 — Host / Host + Paths: several machines, untagged snapshots**

Two machines run plain `restic backup` (no tags) into a shared repo, and `server` backs up two directories:

```
ID        Time                 Host    Tags  Paths
-------------------------------------------------------
c3000001  2026-06-01 00:00:00  laptop        /home/me
d4000001  2026-06-01 00:10:00  server        /etc
e5000001  2026-06-01 00:20:00  server        /var/www
...       (daily, for each host and path)
```

With a `Keep last 7` policy:
- **Host + Paths** keeps the 7 newest snapshots of each `(host, paths)` pair: `laptop:/home/me`, `server:/etc` and `server:/var/www`, so 21 in total.
- **Host** keeps the 7 newest snapshots per machine: 7 for `laptop` and 7 for `server` across both of its paths.
- **Tags** treats all untagged snapshots as one group and keeps only 7 in the whole repo.

::: tip
If you want tag-based grouping for snapshots created by external tools, give them stable tags (e.g. `zfs:challenger/data`) rather than a tag that is unique to each run, such as the source snapshot name.
:::

### ✂️ Prune
[Restic Documentation](https://restic.readthedocs.io/en/latest/060_forget.html#removing-unreferenced-data)

Removes unreferenced data using `restic prune`. Like Backup, Prune operations trigger their respective lifecycle hooks (e.g., `CONDITION_PRUNE_START`).

**Configuration:**
- Scheduled in repo settings
- Appears under `_system_` plan
- **Parameters:**
  - Schedule timing
  - Max unused percent (controls repacking threshold)

::: info
**Optimization Tips:**
- Run infrequently (monthly recommended)
- Use higher max unused percent (5-10%) to reduce repacking
- Consider storage costs vs. cleanup frequency
:::

### 🔍 Check
[Restic Documentation](https://restic.readthedocs.io/en/latest/080_check.html)

Verifies repository integrity using `restic check`.

**Configuration:**
- Scheduled in repo settings
- Appears under `_system_` plan
- **Parameters:**
  - Schedule timing
  - Read data percentage

::: warning
A value of 100% for *read data%* will read/download every pack file in your repository. This can be very slow and, if your provider bills for egress bandwidth, can be expensive. It is recommended to set this to 0% or a low value (e.g. 10%) for most use cases.
:::

### 🗂️ Index Snapshots
[Restic Documentation](https://restic.readthedocs.io/en/latest/045_working_with_repos.html#listing-all-snapshots)

Reads the list of snapshots in the repository using `restic snapshots` and records them in Backrest's operation history. Snapshots that no longer exist in the repository are marked as forgotten.

Backrest indexes a repository:
- after each backup run by one of its plans
- when you click **Index Snapshots** in the repository view
- on the repository's **Index Policy** schedule, if one is set

**Configuration:**
- Scheduled in repo settings (**Index Policy**), disabled by default
- **Parameters:**
  - Schedule timing

Enable the Index Policy for repositories that other machines or tools back up into (for example `resticprofile` or your own scripts calling `restic backup`). Without it, Backrest only notices their snapshots after its own backups or when you index manually.

The index schedule is tracked in memory, so a scheduled index also runs once shortly after Backrest starts.

#### Sources

The **Sources** tab of the repository view groups the indexed snapshots by **source**: the hostname and the exact set of backed-up paths. This is the same grouping restic uses for `restic forget --group-by host,paths`:

- Paths are sorted, so `restic backup /data /etc` and `restic backup /etc /data` are the same source.
- A snapshot of `[/data, /etc]` is a different source from a snapshot of `[/data]`. A restic snapshot is a single unit, so it belongs to exactly one source.
- Hostnames are compared exactly, including case.
- Tags are not part of the source. Snapshots created by a Backrest plan show the plan; snapshots without a `plan:` tag are shown as **External**.

For each source, the table shows the number of snapshots, the time of the latest snapshot and its size (the amount of data restic processed for it, before deduplication).