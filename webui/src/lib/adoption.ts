import { clone } from "@bufbuild/protobuf";
import {
  AdoptedSource,
  Repo,
  RepoSchema,
  RetentionPolicy,
  Schedule,
} from "../../gen/ts/v1/config_pb";
import { SnapshotSource } from "../../gen/ts/v1/service_pb";
import * as m from "../paraglide/messages";

// Mirrors internal/adoption on the backend, which is the source of truth.

// ruleMatchesSource reports whether an adopted source rule's selector matches
// the source: exact hostname, and the rule's path is one of the source's paths.
export const ruleMatchesSource = (
  rule: Pick<AdoptedSource, "hostname" | "path">,
  source: Pick<SnapshotSource, "hostname" | "paths">,
): boolean =>
  rule.hostname === source.hostname && source.paths.includes(rule.path);

// sameSelector reports whether two rules have the same selector (hostname and
// path). The selector identifies a rule.
export const sameSelector = (
  a: Pick<AdoptedSource, "hostname" | "path">,
  b: Pick<AdoptedSource, "hostname" | "path">,
): boolean => a.hostname === b.hostname && a.path === b.path;

// withRule returns a copy of the repo with the rule added.
export const withRule = (repo: Repo, rule: AdoptedSource): Repo => {
  const next = clone(RepoSchema, repo);
  next.adoptedSources = [...next.adoptedSources, rule];
  return next;
};

// withoutRule returns a copy of the repo without the rule with the given selector.
export const withoutRule = (
  repo: Repo,
  rule: Pick<AdoptedSource, "hostname" | "path">,
): Repo => {
  const next = clone(RepoSchema, repo);
  next.adoptedSources = next.adoptedSources.filter(
    (r) => !sameSelector(r, rule),
  );
  return next;
};

export const forgetPolicyEnabled = (repo: Repo): boolean =>
  scheduleEnabled(repo.forgetPolicy?.schedule);

export const scheduleEnabled = (schedule: Schedule | undefined): boolean =>
  !!schedule &&
  schedule.schedule.case !== undefined &&
  schedule.schedule.case !== "disabled";

export const retentionSummary = (
  retention: RetentionPolicy | undefined,
): string => {
  const p = retention?.policy;
  switch (p?.case) {
    case "policyKeepLastN":
      return m.sources_retention_keep_last({ count: p.value });
    case "policyTimeBucketed":
      return m.sources_retention_buckets({
        hourly: p.value.hourly,
        daily: p.value.daily,
        weekly: p.value.weekly,
        monthly: p.value.monthly,
        yearly: p.value.yearly,
        last: p.value.keepLastN,
      });
    default:
      return "–";
  }
};
