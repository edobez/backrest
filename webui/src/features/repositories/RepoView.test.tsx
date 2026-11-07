import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as m from "../../paraglide/messages";
import { RepoView } from "./RepoView";
import { renderWithProviders } from "../../test/render";
import { makeConfig, makeRepo } from "../../test/proto";
import { backrestService } from "../../api/client";
import { RepoMode } from "../../../gen/ts/v1/config_pb";
import { DoRepoTaskRequest_Task } from "../../../gen/ts/v1/service_pb";

// The operation views need DOM primitives jsdom lacks; they are not under test.
vi.mock("../operations/OperationListView", () => ({
  OperationListView: () => <div data-testid="operation-list-view-stub" />,
}));
vi.mock("../operations/OperationTreeView", () => ({
  OperationTreeView: () => <div data-testid="operation-tree-view-stub" />,
}));

const renderRepo = (mode: RepoMode) => {
  const repo = makeRepo({ id: "test-repo", guid: "test-repo-guid", mode });
  const config = makeConfig({ repos: [repo] });
  return renderWithProviders(<RepoView repo={repo} />, { config });
};

const openMenu = async (
  user: ReturnType<typeof renderWithProviders>["user"],
) => {
  await user.click(await screen.findByRole("button", { name: "More actions" }));
};

const menuItem = async (label: string) =>
  (await screen.findByText(label)).closest("[role=menuitem]") as HTMLElement;

describe("RepoView repo modes", () => {
  it("shows no badge and runs check directly for a managed repo", async () => {
    vi.mocked(backrestService.doRepoTask).mockResolvedValue({} as any);
    const { user } = renderRepo(RepoMode.MANAGED);

    expect(screen.queryByTestId("repo-mode-badge")).not.toBeInTheDocument();
    await openMenu(user);
    await user.click(await screen.findByText(m.repo_button_check()));

    await waitFor(() =>
      expect(backrestService.doRepoTask).toHaveBeenCalledWith(
        expect.objectContaining({ task: DoRepoTaskRequest_Task.CHECK }),
      ),
    );
    expect(
      screen.queryByText(m.repo_check_lock_title()),
    ).not.toBeInTheDocument();
  });

  it("disables prune and unlock for an append-only repo", async () => {
    const { user } = renderRepo(RepoMode.APPEND_ONLY);

    expect(await screen.findByTestId("repo-mode-badge")).toHaveTextContent(
      m.repo_mode_append_only(),
    );
    await openMenu(user);
    expect(await menuItem(m.repo_button_prune())).toHaveAttribute(
      "data-disabled",
    );
    expect(await menuItem(m.repo_button_unlock())).toHaveAttribute(
      "data-disabled",
    );
    expect(await menuItem(m.repo_button_check())).not.toHaveAttribute(
      "data-disabled",
    );
  });

  it("asks for confirmation before checking a read-only repo", async () => {
    vi.mocked(backrestService.doRepoTask).mockResolvedValue({} as any);
    const { user } = renderRepo(RepoMode.READ_ONLY);

    expect(await screen.findByTestId("repo-mode-badge")).toHaveTextContent(
      m.repo_mode_read_only(),
    );
    await openMenu(user);
    await user.click(await screen.findByText(m.repo_button_check()));

    expect(
      await screen.findByText(m.repo_check_lock_title()),
    ).toBeInTheDocument();
    expect(backrestService.doRepoTask).not.toHaveBeenCalled();

    await user.click(screen.getByTestId("confirm-check-lock"));
    await waitFor(() =>
      expect(backrestService.doRepoTask).toHaveBeenCalledWith(
        expect.objectContaining({
          repoId: "test-repo",
          task: DoRepoTaskRequest_Task.CHECK,
        }),
      ),
    );
  });
});
