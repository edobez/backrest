import { afterEach, describe, expect, it, vi } from "vitest";
import {
  formatBytes,
  formatDate,
  formatDuration,
  formatMonth,
  formatTime,
  getDateFormatPreference,
  localISOTime,
  setDateFormatPreference,
  subscribeDateFormat,
  DATE_FORMAT_STORAGE_KEY,
  normalizeSnapshotId,
} from "./formatting";

describe("formatBytes", () => {
  it("returns 0B for zero, undefined, and falsy inputs", () => {
    expect(formatBytes(0)).toBe("0B");
    expect(formatBytes(undefined)).toBe("0B");
  });

  it("keeps small values in bytes", () => {
    expect(formatBytes(1)).toBe("1 B");
    expect(formatBytes(1023)).toBe("1023 B");
  });

  it("does not convert at exactly 1024 (strict > boundary)", () => {
    expect(formatBytes(1024)).toBe("1024 B");
  });

  it("crosses the KiB boundary just above 1024", () => {
    expect(formatBytes(1025)).toBe("1 KiB");
  });

  it("does not convert at exactly 1024 KiB", () => {
    expect(formatBytes(1024 * 1024)).toBe("1024 KiB");
  });

  it("crosses the MiB boundary just above 1024 KiB", () => {
    expect(formatBytes(1024 * 1024 + 1)).toBe("1 MiB");
  });

  it("does not convert at exactly 1024 MiB", () => {
    expect(formatBytes(1024 * 1024 * 1024)).toBe("1024 MiB");
  });

  it("crosses the GiB boundary just above 1024 MiB", () => {
    expect(formatBytes(1024 * 1024 * 1024 + 1)).toBe("1 GiB");
  });

  it("rounds to 2 decimal places", () => {
    // 1500 bytes -> 1.46484375 KiB, rounded to 1.46
    expect(formatBytes(1500)).toBe("1.46 KiB");
  });

  it("parses string input", () => {
    expect(formatBytes("2048")).toBe("2 KiB");
  });
});

describe("formatDuration", () => {
  it("returns an empty string for falsy, non-zero-safe input", () => {
    expect(formatDuration(undefined as unknown as number)).toBe("");
    expect(formatDuration(null as unknown as number)).toBe("");
  });

  it("treats an explicit zero duration as 0 of the min unit", () => {
    expect(formatDuration(0)).toBe("0s");
  });

  it("formats sub-minute durations in seconds (fast path)", () => {
    expect(formatDuration(1000)).toBe("1s");
    expect(formatDuration(45_000)).toBe("45s");
    expect(formatDuration(59_000)).toBe("59s");
  });

  it("formats minutes and seconds once at or above a minute", () => {
    expect(formatDuration(60_000)).toBe("1m");
    expect(formatDuration(90_000)).toBe("1m30s");
  });

  it("formats hours, minutes, and seconds", () => {
    expect(formatDuration(3_661_000)).toBe("1h1m1s");
  });

  it("formats negative durations with a leading minus sign (fast path applies below 60s magnitude check)", () => {
    // Note: the fast path only checks `ms < 60_000`, which is also true for any
    // negative value, so negative durations always render in seconds unless
    // options are supplied.
    expect(formatDuration(-90_000)).toBe("-90s");
  });

  it("respects minUnit to drop smaller units", () => {
    expect(formatDuration(3_661_000, { minUnit: "m" })).toBe("1h1m");
    expect(formatDuration(-90_000, { minUnit: "m" })).toBe("-1m");
  });

  it("respects maxUnit to drop larger units", () => {
    // 90_000_000ms = 1 day 1 hour; capping at "h" hides the day component.
    expect(formatDuration(90_000_000)).toBe("1d1h");
    expect(formatDuration(90_000_000, { maxUnit: "h" })).toBe("1h");
  });
});

describe("normalizeSnapshotId", () => {
  it("truncates a snapshot id to 8 characters", () => {
    expect(normalizeSnapshotId("abcdef1234567890")).toBe("abcdef12");
  });

  it("returns the whole string when shorter than 8 characters", () => {
    expect(normalizeSnapshotId("abc")).toBe("abc");
  });

  it("returns an empty string for empty input", () => {
    expect(normalizeSnapshotId("")).toBe("");
  });
});

describe("formatDate", () => {
  it("formats a fixed timestamp as a date-only string", () => {
    // 2021-01-15T00:00:00.000Z
    const result = formatDate(1610668800000);
    expect(result).toMatch(/^\d{2}\/\d{2}\/\d{4}$/);
  });

  it("accepts string and Date inputs equivalently to number", () => {
    const ms = 1610668800000;
    expect(formatDate(String(ms))).toBe(formatDate(ms));
    expect(formatDate(new Date(ms))).toBe(formatDate(ms));
  });
});

describe("formatMonth", () => {
  it("formats a fixed timestamp as a month/year string", () => {
    // 2021-01-15T00:00:00.000Z
    const result = formatMonth(1610668800000);
    expect(result).toMatch(/^\d{2}\/\d{4}$/);
  });
});

describe("formatTime", () => {
  it("formats a fixed timestamp including date and time components", () => {
    const result = formatTime(1610668800000);
    // Locale-dependent, but should contain a date and an hour:minute pairing.
    expect(result).toMatch(/\d{2}\/\d{2}\/\d{4}/);
    expect(result).toMatch(/\d{1,2}:\d{2}/);
  });
});

describe("localISOTime", () => {
  it("returns a valid ISO 8601 string", () => {
    const result = localISOTime(1610668800000);
    expect(result).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
  });

  it("is consistent across number, string, and Date input forms", () => {
    const ms = 1610668800000;
    expect(localISOTime(String(ms))).toBe(localISOTime(ms));
    expect(localISOTime(new Date(ms))).toBe(localISOTime(ms));
  });
});

describe("date format preference", () => {
  // Built from local components so the tests are timezone-independent.
  const afternoon = new Date(2026, 10, 6, 14, 30);
  const morning = new Date(2026, 10, 6, 9, 5);

  afterEach(() => {
    setDateFormatPreference({ style: "browser", hourCycle: "auto" });
    localStorage.clear();
  });

  it("defaults to browser style with automatic clock", () => {
    expect(getDateFormatPreference()).toEqual({
      style: "browser",
      hourCycle: "auto",
    });
  });

  it("formats ISO 8601 in local time, always 24h", () => {
    setDateFormatPreference({ style: "iso", hourCycle: "h12" });
    expect(formatTime(afternoon)).toBe("2026-11-06 14:30");
    expect(formatTime(morning)).toBe("2026-11-06 09:05");
    expect(formatDate(afternoon)).toBe("2026-11-06");
    expect(formatMonth(afternoon)).toBe("2026-11");
  });

  it("formats by locale", () => {
    setDateFormatPreference({
      style: "locale",
      locale: "en-US",
      hourCycle: "auto",
    });
    expect(formatDate(afternoon)).toBe("11/06/2026");
    expect(formatMonth(afternoon)).toBe("11/2026");
    setDateFormatPreference({
      style: "locale",
      locale: "de-DE",
      hourCycle: "auto",
    });
    expect(formatDate(afternoon)).toBe("06.11.2026");
    expect(formatMonth(afternoon)).toMatch(/^11[./]2026$/);
    setDateFormatPreference({
      style: "locale",
      locale: "en-GB",
      hourCycle: "auto",
    });
    expect(formatDate(afternoon)).toBe("06/11/2026");
  });

  it("honours 12h and 24h clock for locale style", () => {
    setDateFormatPreference({
      style: "locale",
      locale: "en-US",
      hourCycle: "h12",
    });
    expect(formatTime(afternoon)).toMatch(/2:30\s?PM/);
    setDateFormatPreference({
      style: "locale",
      locale: "en-US",
      hourCycle: "h23",
    });
    expect(formatTime(afternoon)).toMatch(/14:30/);
    expect(formatTime(afternoon)).not.toMatch(/PM/);
    setDateFormatPreference({
      style: "locale",
      locale: "de-DE",
      hourCycle: "h12",
    });
    expect(formatTime(afternoon)).toMatch(/2:30/);
  });

  it("honours the clock option for browser style", () => {
    setDateFormatPreference({ style: "browser", hourCycle: "h23" });
    expect(formatTime(afternoon)).toMatch(/14:30/);
    setDateFormatPreference({ style: "browser", hourCycle: "h12" });
    expect(formatTime(afternoon)).toMatch(/2:30/);
  });

  it("falls back to the browser default for an invalid locale", () => {
    setDateFormatPreference({
      style: "locale",
      locale: "not a locale!!",
      hourCycle: "auto",
    });
    expect(() => formatTime(afternoon)).not.toThrow();
    setDateFormatPreference({ style: "browser", hourCycle: "auto" });
    const expected = formatTime(afternoon);
    setDateFormatPreference({
      style: "locale",
      locale: "not a locale!!",
      hourCycle: "auto",
    });
    expect(formatTime(afternoon)).toBe(expected);
    expect(formatDate(afternoon)).toMatch(/2026/);
  });

  it("rebuilds formatters after the preference changes", () => {
    setDateFormatPreference({
      style: "locale",
      locale: "de-DE",
      hourCycle: "auto",
    });
    const de = formatDate(afternoon);
    expect(formatDate(afternoon)).toBe(de); // memoized path
    setDateFormatPreference({
      style: "locale",
      locale: "en-US",
      hourCycle: "auto",
    });
    expect(formatDate(afternoon)).not.toBe(de);
  });

  it("does not change localISOTime", () => {
    const before = localISOTime(afternoon);
    setDateFormatPreference({ style: "iso", hourCycle: "auto" });
    expect(localISOTime(afternoon)).toBe(before);
  });

  it("notifies subscribers and supports unsubscribe", () => {
    const listener = vi.fn();
    const unsubscribe = subscribeDateFormat(listener);
    setDateFormatPreference({ style: "iso", hourCycle: "auto" });
    expect(listener).toHaveBeenCalledTimes(1);
    unsubscribe();
    setDateFormatPreference({ style: "browser", hourCycle: "auto" });
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("persists to localStorage and sanitizes input", () => {
    setDateFormatPreference({
      style: "locale",
      locale: "fr-FR",
      hourCycle: "h23",
    });
    expect(JSON.parse(localStorage.getItem(DATE_FORMAT_STORAGE_KEY)!)).toEqual({
      style: "locale",
      locale: "fr-FR",
      hourCycle: "h23",
    });
    setDateFormatPreference({ style: "bogus", hourCycle: "x" } as any);
    expect(getDateFormatPreference()).toEqual({
      style: "browser",
      hourCycle: "auto",
    });
  });
});
