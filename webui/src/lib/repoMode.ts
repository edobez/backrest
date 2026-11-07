import { RepoMode } from "../../gen/ts/v1/config_pb";
import * as m from "../paraglide/messages";

// Mirrors internal/repomode on the backend, which is the source of truth.
// The UI uses it only to hide or disable actions the backend would refuse.
export type RepoAction =
  "backup" | "forget" | "prune" | "unlock" | "scheduledCheck" | "manualCheck";

export const repoModeAllows = (
  mode: RepoMode | undefined,
  action: RepoAction,
): boolean => {
  switch (mode) {
    case RepoMode.APPEND_ONLY:
      return !["forget", "prune", "unlock"].includes(action);
    case RepoMode.READ_ONLY:
      return action === "manualCheck";
    default:
      return true;
  }
};

// JSON names of the enum values, as used by the repo form (toJson/fromJson).
export const repoModeJsonNames: Record<RepoMode, string> = {
  [RepoMode.MANAGED]: "REPO_MODE_MANAGED",
  [RepoMode.APPEND_ONLY]: "REPO_MODE_APPEND_ONLY",
  [RepoMode.READ_ONLY]: "REPO_MODE_READ_ONLY",
};

export const repoModeFromJsonName = (name: string | undefined): RepoMode => {
  const entry = Object.entries(repoModeJsonNames).find(([, n]) => n === name);
  return entry ? (Number(entry[0]) as RepoMode) : RepoMode.MANAGED;
};

export const repoModeLabel = (mode: RepoMode | undefined): string => {
  switch (mode) {
    case RepoMode.APPEND_ONLY:
      return m.repo_mode_append_only();
    case RepoMode.READ_ONLY:
      return m.repo_mode_read_only();
    default:
      return m.repo_mode_managed();
  }
};

// Explanation shown on disabled actions.
export const repoModeDisallowedReason = (mode: RepoMode | undefined): string =>
  m.repo_mode_action_not_allowed({ mode: repoModeLabel(mode) });
