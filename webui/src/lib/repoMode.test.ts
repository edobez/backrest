import { describe, expect, it } from "vitest";
import { RepoMode } from "../../gen/ts/v1/config_pb";
import {
  RepoAction,
  repoModeAllows,
  repoModeFromJsonName,
  repoModeJsonNames,
} from "./repoMode";

const actions: RepoAction[] = [
  "backup",
  "forget",
  "prune",
  "unlock",
  "scheduledCheck",
  "manualCheck",
];

describe("repoModeAllows", () => {
  it("allows everything when managed or unset", () => {
    for (const a of actions) {
      expect(repoModeAllows(RepoMode.MANAGED, a)).toBe(true);
      expect(repoModeAllows(undefined, a)).toBe(true);
    }
  });

  it("forbids removals when append-only", () => {
    const allowed = actions.filter((a) =>
      repoModeAllows(RepoMode.APPEND_ONLY, a),
    );
    expect(allowed).toEqual(["backup", "scheduledCheck", "manualCheck"]);
  });

  it("only allows the manual check when read-only", () => {
    const allowed = actions.filter((a) =>
      repoModeAllows(RepoMode.READ_ONLY, a),
    );
    expect(allowed).toEqual(["manualCheck"]);
  });
});

describe("repoModeFromJsonName", () => {
  it("round-trips the JSON names", () => {
    for (const mode of [
      RepoMode.MANAGED,
      RepoMode.APPEND_ONLY,
      RepoMode.READ_ONLY,
    ]) {
      expect(repoModeFromJsonName(repoModeJsonNames[mode])).toBe(mode);
    }
  });

  it("defaults to managed", () => {
    expect(repoModeFromJsonName(undefined)).toBe(RepoMode.MANAGED);
    expect(repoModeFromJsonName("bogus")).toBe(RepoMode.MANAGED);
  });
});
