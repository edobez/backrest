export const uios = (process.env.UI_OS || "").trim().toLowerCase();
export const isWindows = uios === "windows";
export const uiBuildVersion = (
  process.env.BACKREST_BUILD_VERSION || "dev-snapshot-build"
).trim();
// True unless BACKREST_BUILD_VERSION was explicitly set (i.e. a release build).
// uiBuildVersion may still be derived from `git describe` in dev builds.
export const isDevBuild = !(process.env.BACKREST_BUILD_VERSION_EXPLICIT === "true");
export const pathSeparator = isWindows ? "\\" : "/";
export const backendUrl = process.env.UI_BACKEND_URL || "./";
console.log(
  `UI OS: ${uios}, Build Version: ${uiBuildVersion}, Backend URL: ${backendUrl}`,
);
export const features = new Set<string>(
  (process.env.UI_FEATURES || "").split(","),
);

// Feature flags
export const isMultihostSyncEnabled = true;
