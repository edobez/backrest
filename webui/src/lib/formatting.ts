import { useSyncExternalStore } from "react";

const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
export const formatBytes = (bytes?: number | string) => {
  if (!bytes) {
    return "0B";
  }
  if (typeof bytes === "string") {
    bytes = parseInt(bytes);
  }

  let unit = 0;
  while (bytes > 1024) {
    bytes /= 1024;
    unit++;
  }
  return `${Math.round(bytes * 100) / 100} ${units[unit]}`;
};

export type DateFormatStyle = "browser" | "iso" | "locale";
export type DateFormatHourCycle = "auto" | "h12" | "h23";
export interface DateFormatPreference {
  style: DateFormatStyle;
  // BCP 47 locale tag, only used when style is "locale".
  locale?: string;
  hourCycle: DateFormatHourCycle;
}

export const DATE_FORMAT_STORAGE_KEY = "BACKREST_DATE_FORMAT";

const defaultDateFormatPreference = (): DateFormatPreference => ({
  style: "browser",
  hourCycle: "auto",
});

const sanitizeDateFormatPreference = (raw: any): DateFormatPreference => {
  const out = defaultDateFormatPreference();
  if (!raw || typeof raw !== "object") return out;
  if (
    raw.style === "browser" ||
    raw.style === "iso" ||
    raw.style === "locale"
  ) {
    out.style = raw.style;
  }
  if (
    raw.hourCycle === "auto" ||
    raw.hourCycle === "h12" ||
    raw.hourCycle === "h23"
  ) {
    out.hourCycle = raw.hourCycle;
  }
  if (typeof raw.locale === "string" && raw.locale) {
    out.locale = raw.locale;
  }
  if (out.style === "locale" && !out.locale) {
    out.style = "browser";
  }
  return out;
};

const loadDateFormatPreference = (): DateFormatPreference => {
  try {
    const raw = globalThis.localStorage?.getItem(DATE_FORMAT_STORAGE_KEY);
    if (raw) return sanitizeDateFormatPreference(JSON.parse(raw));
  } catch {
    // ignore unreadable or malformed storage
  }
  return defaultDateFormatPreference();
};

let currentDateFormat: DateFormatPreference = loadDateFormatPreference();
let dateFormatVersion = 0;
const dateFormatListeners = new Set<() => void>();
const formatterCache = new Map<string, Intl.DateTimeFormat>();

export const getDateFormatPreference = (): DateFormatPreference =>
  currentDateFormat;

export const getDateFormatVersion = (): number => dateFormatVersion;

export const setDateFormatPreference = (p: DateFormatPreference) => {
  currentDateFormat = sanitizeDateFormatPreference(p);
  formatterCache.clear();
  dateFormatVersion++;
  try {
    globalThis.localStorage?.setItem(
      DATE_FORMAT_STORAGE_KEY,
      JSON.stringify(currentDateFormat),
    );
  } catch {
    // storage unavailable; the preference still applies for this session
  }
  dateFormatListeners.forEach((l) => l());
};

export const subscribeDateFormat = (listener: () => void) => {
  dateFormatListeners.add(listener);
  return () => {
    dateFormatListeners.delete(listener);
  };
};

// useDateFormat re-renders the calling component when the date format
// preference changes. The returned version can be used as a hook dependency
// for memoized values that embed formatted dates.
export const useDateFormat = (): number =>
  useSyncExternalStore(subscribeDateFormat, getDateFormatVersion);

// usePreferredDateFormat returns the current preference object.
export const useDateFormatPreference = (): DateFormatPreference =>
  useSyncExternalStore(subscribeDateFormat, getDateFormatPreference);

type FormatKind = "time" | "date" | "month";

const buildFormatter = (
  kind: FormatKind,
  locale: string | undefined,
  hourCycle: DateFormatHourCycle,
): Intl.DateTimeFormat => {
  const opts: Intl.DateTimeFormatOptions =
    kind === "time"
      ? {
          hour: "2-digit",
          minute: "2-digit",
          day: "2-digit",
          month: "2-digit",
          year: "numeric",
        }
      : kind === "date"
        ? { year: "numeric", month: "2-digit", day: "2-digit" }
        : { year: "numeric", month: "2-digit" };
  if (kind === "time") {
    if (hourCycle === "h12") opts.hour12 = true;
    else if (hourCycle === "h23") opts.hourCycle = "h23";
  }
  try {
    return new Intl.DateTimeFormat(locale, opts);
  } catch {
    // invalid locale tag: fall back to the browser default
    return new Intl.DateTimeFormat(undefined, opts);
  }
};

const getFormatter = (
  kind: FormatKind,
  pref: DateFormatPreference,
): Intl.DateTimeFormat => {
  const { style, locale, hourCycle } = pref;
  const loc = style === "locale" ? locale : undefined;
  const key = `${kind}|${loc ?? ""}|${hourCycle}`;
  let f = formatterCache.get(key);
  if (!f) {
    f = buildFormatter(kind, loc, hourCycle);
    formatterCache.set(key, f);
  }
  return f;
};

const toDate = (time: number | string | Date): Date => {
  if (typeof time === "string") {
    time = parseInt(time);
  } else if (time instanceof Date) {
    time = time.getTime();
  }
  return new Date(time);
};

const pad = (n: number, len = 2) => String(n).padStart(len, "0");
const isoDate = (d: Date) =>
  `${pad(d.getFullYear(), 4)}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

const timezoneOffsetMs = new Date().getTimezoneOffset() * 60 * 1000;
// formatTime formats a date and time according to the user's date format
// preference. Pass pref to format with a preference that is not applied yet
// (e.g. a preview of unsaved settings).
export const formatTime = (
  time: number | string | Date,
  pref: DateFormatPreference = currentDateFormat,
) => {
  const d = toDate(time);
  if (pref.style === "iso") {
    return `${isoDate(d)} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }
  return getFormatter("time", pref).format(d);
};

export const localISOTime = (time: number | string | Date) => {
  if (typeof time === "string") {
    time = parseInt(time);
  } else if (time instanceof Date) {
    time = time.getTime();
  }

  const d = new Date();
  d.setTime(time - timezoneOffsetMs);
  return d.toISOString();
};

export const formatDate = (time: number | string | Date) => {
  const d = toDate(time);
  if (currentDateFormat.style === "iso") return isoDate(d);
  return getFormatter("date", currentDateFormat).format(d);
};

export const formatMonth = (time: number | string | Date) => {
  const d = toDate(time);
  if (currentDateFormat.style === "iso") return isoDate(d).slice(0, 7);
  return getFormatter("month", currentDateFormat).format(d);
};

const durationSteps = [1000, 60, 60, 24, Number.MAX_VALUE];
const durationFactors = [
  1,
  1000,
  60 * 1000,
  60 * 60 * 1000,
  24 * 60 * 60 * 1000,
];
const shortDurationUnits = ["ms", "s", "m", "h", "d"];
type DurationUnit = (typeof shortDurationUnits)[number];

export interface FormatDurationOptions {
  minUnit?: DurationUnit;
  maxUnit?: DurationUnit;
}

export const formatDuration = (ms: number, options?: FormatDurationOptions) => {
  if (!ms && ms !== 0) return "";

  if (!options && ms < 60 * 1000) {
    // If no options and less than a minute, show seconds
    // Performance optimization
    return `${Math.round(ms / 1000)}s`;
  }

  const minUnitIndex = options?.minUnit
    ? shortDurationUnits.indexOf(options.minUnit)
    : 1; // Don't show ms by default
  const maxUnitIndex = options?.maxUnit
    ? shortDurationUnits.indexOf(options.maxUnit)
    : shortDurationUnits.length - 1;

  const absMs = Math.abs(ms);
  let result = "";

  for (let i = maxUnitIndex; i >= minUnitIndex; i--) {
    const value = Math.floor(absMs / durationFactors[i]) % durationSteps[i];
    if (value > 0) {
      result += `${value}${shortDurationUnits[i]}`;
    }
  }

  if (!result) {
    result = `0${shortDurationUnits[minUnitIndex]}`;
  }

  return ms < 0 ? `-${result}` : result;
};

export const normalizeSnapshotId = (id: string) => {
  return id.substring(0, 8);
};
