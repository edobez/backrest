import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  AdoptedSourceSchema,
  RetentionPolicySchema,
} from "../../gen/ts/v1/config_pb";
import { makeRepo } from "../test/proto";
import {
  forgetPolicyEnabled,
  retentionSummary,
  ruleMatchesSource,
  withoutRule,
  withRule,
} from "./adoption";

describe("adoption helpers", () => {
  const rule = create(AdoptedSourceSchema, { hostname: "nas", path: "/data" });

  it("matches sources by exact host and contained path", () => {
    expect(ruleMatchesSource(rule, { hostname: "nas", paths: ["/data"] })).toBe(
      true,
    );
    expect(
      ruleMatchesSource(rule, { hostname: "nas", paths: ["/data", "/etc"] }),
    ).toBe(true);
    expect(
      ruleMatchesSource(rule, { hostname: "nas", paths: ["/data/sub"] }),
    ).toBe(false);
    expect(ruleMatchesSource(rule, { hostname: "NAS", paths: ["/data"] })).toBe(
      false,
    );
  });

  it("adds and removes rules without mutating the repo", () => {
    const repo = makeRepo();
    const added = withRule(repo, rule);
    expect(repo.adoptedSources).toHaveLength(0);
    expect(added.adoptedSources).toHaveLength(1);
    const removed = withoutRule(added, { hostname: "nas", path: "/data" });
    expect(removed.adoptedSources).toHaveLength(0);
    expect(added.adoptedSources).toHaveLength(1);
  });

  it("detects an enabled repo forget policy", () => {
    expect(forgetPolicyEnabled(makeRepo())).toBe(false);
    expect(
      forgetPolicyEnabled(
        makeRepo({
          forgetPolicy: {
            schedule: { schedule: { case: "disabled", value: true } },
          },
        }),
      ),
    ).toBe(false);
    expect(
      forgetPolicyEnabled(
        makeRepo({
          forgetPolicy: {
            schedule: { schedule: { case: "maxFrequencyDays", value: 1 } },
          },
        }),
      ),
    ).toBe(true);
  });

  it("summarizes retention", () => {
    expect(
      retentionSummary(
        create(RetentionPolicySchema, {
          policy: { case: "policyKeepLastN", value: 5 },
        }),
      ),
    ).toContain("5");
  });
});
