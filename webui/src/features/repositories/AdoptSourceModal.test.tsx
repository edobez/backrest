import { screen, waitFor } from "@testing-library/react";
import { create } from "@bufbuild/protobuf";
import { describe, expect, it, vi } from "vitest";
import * as m from "../../paraglide/messages";
import { AdoptSourceModal } from "./AdoptSourceModal";
import { renderWithProviders } from "../../test/render";
import { backrestService } from "../../api/client";
import { makeConfig, makeRepo } from "../../test/proto";
import { PreviewAdoptedSourceResponseSchema } from "../../../gen/ts/v1/service_pb";

const previewResponse = () =>
  create(PreviewAdoptedSourceResponseSchema, {
    matches: [
      {
        source: { id: "a", hostname: "nas", paths: ["/data"] },
        keep: [{ id: "k1" }],
        remove: [{ id: "r1" }, { id: "r2" }],
      },
      {
        source: { id: "b", hostname: "nas", paths: ["/data", "/etc"] },
        keep: [{ id: "k2" }],
        refusedReason:
          "source mixes 1 snapshot(s) of plan p with 1 external snapshot(s)",
      },
    ],
  });

describe("AdoptSourceModal", () => {
  it("requires a preview of the current settings before adopting", async () => {
    const repo = makeRepo();
    vi.mocked(backrestService.previewAdoptedSource).mockResolvedValue(
      previewResponse(),
    );
    vi.mocked(backrestService.addRepo).mockResolvedValue(makeConfig());

    const { user, setConfig } = renderWithProviders(
      <AdoptSourceModal repo={repo} hostname="nas" paths={["/data"]} />,
      { config: makeConfig({ repos: [repo] }) },
    );

    const submit = await screen.findByTestId("adopt-submit");
    expect(submit).toBeDisabled();

    await user.click(screen.getByTestId("adopt-preview"));
    await screen.findByTestId("adopt-preview-table");
    expect(backrestService.previewAdoptedSource).toHaveBeenCalledWith(
      expect.objectContaining({
        repoId: repo.id,
        rule: expect.objectContaining({ hostname: "nas", path: "/data" }),
      }),
    );
    expect(
      screen.getByText(
        m.adopt_modal_preview_summary({ sources: 1, remove: 2 }),
      ),
    ).toBeInTheDocument();
    expect(screen.getByText(/Refused:/)).toBeInTheDocument();

    // Editing the selector invalidates the preview.
    await user.type(screen.getByLabelText(m.adopt_modal_hostname()), "2");
    expect(screen.getByTestId("adopt-submit")).toBeDisabled();
    await user.type(
      screen.getByLabelText(m.adopt_modal_hostname()),
      "{Backspace}",
    );
    await user.click(screen.getByTestId("adopt-preview"));
    await waitFor(() =>
      expect(screen.getByTestId("adopt-submit")).toBeEnabled(),
    );

    // ConfirmButton: first click arms, second click submits.
    await user.click(screen.getByTestId("adopt-submit"));
    await user.click(screen.getByTestId("adopt-submit"));
    await waitFor(() => expect(backrestService.addRepo).toHaveBeenCalled());
    const req = vi.mocked(backrestService.addRepo).mock.calls[0][0] as any;
    expect(req.repo.adoptedSources).toHaveLength(1);
    expect(req.repo.adoptedSources[0]).toMatchObject({
      hostname: "nas",
      path: "/data",
    });
    expect(setConfig).toHaveBeenCalled();
  });

  it("blocks adopting when the repo forget policy is enabled", async () => {
    const repo = makeRepo({
      forgetPolicy: {
        schedule: { schedule: { case: "maxFrequencyDays", value: 1 } },
      },
    });
    vi.mocked(backrestService.previewAdoptedSource).mockResolvedValue(
      previewResponse(),
    );
    const { user } = renderWithProviders(
      <AdoptSourceModal repo={repo} hostname="nas" paths={["/data"]} />,
      { config: makeConfig({ repos: [repo] }) },
    );
    expect(await screen.findByTestId("adopt-conflict")).toBeInTheDocument();
    await user.click(screen.getByTestId("adopt-preview"));
    await screen.findByTestId("adopt-preview-table");
    expect(screen.getByTestId("adopt-submit")).toBeDisabled();
  });
});
