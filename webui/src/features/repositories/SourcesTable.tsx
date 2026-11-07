import React, { useCallback, useEffect, useRef, useState } from "react";
import { Badge, Box, Flex, Stack, Table, Text } from "@chakra-ui/react";
import { create } from "@bufbuild/protobuf";
import {
  AddRepoRequestSchema,
  ListSourcesRequestSchema,
  SnapshotSource,
} from "../../../gen/ts/v1/service_pb";
import { AdoptedSource, Repo } from "../../../gen/ts/v1/config_pb";
import { backrestService } from "../../api/client";
import { operationsStream } from "../../api/oplog";
import { useConfig } from "../../app/provider";
import { formatBytes, formatTime, useDateFormat } from "../../lib/formatting";
import {
  retentionSummary,
  ruleMatchesSource,
  scheduleEnabled,
  withoutRule,
} from "../../lib/adoption";
import { repoModeAllows, repoModeDisallowedReason } from "../../lib/repoMode";
import { alerts, formatErrorAlert } from "../../components/common/Alerts";
import { useShowModal } from "../../components/common/ModalManager";
import { ConfirmButton } from "../../components/common/SpinButton";
import { Button } from "../../components/ui/button";
import { Tooltip } from "../../components/ui/tooltip";
import { AdoptSourceModal } from "./AdoptSourceModal";
import * as m from "../../paraglide/messages";

// Reload at most this often while a burst of index operations streams in.
const RELOAD_DEBOUNCE_MS = 500;

/**
 * SourcesTable lists the snapshot sources (host + paths, as restic's
 * `--group-by host,paths`) of a repo, computed by the backend from the
 * indexed snapshots. It reloads when snapshots of the repo are (re)indexed.
 * It also lists the repo's adoption rules, and offers to adopt external
 * sources (those without plan snapshots).
 */
export const SourcesTable = ({ repo }: { repo: Repo }) => {
  const repoId = repo.id;
  const repoGuid = repo.guid;
  const showModal = useShowModal();
  const canAdopt = repoModeAllows(repo.mode, "forget");
  useDateFormat(); // re-render when the date format preference changes
  const [sources, setSources] = useState<SnapshotSource[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const load = useCallback(async () => {
    try {
      const resp = await backrestService.listSources(
        create(ListSourcesRequestSchema, { repoId }),
      );
      setSources(resp.sources);
      setError(null);
    } catch (e: any) {
      setError(e?.message ?? String(e));
    }
  }, [repoId]);

  useEffect(() => {
    const scheduleReload = () => {
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(load, RELOAD_DEBOUNCE_MS);
    };
    const unsubscribe = operationsStream.subscribe({
      onMessage: (event) => {
        if (
          event.event.case !== "createdOperations" &&
          event.event.case !== "updatedOperations"
        ) {
          return;
        }
        const touchesIndex = event.event.value.operations.some(
          (op) =>
            op.repoGuid === repoGuid && op.op.case === "operationIndexSnapshot",
        );
        if (touchesIndex) scheduleReload();
      },
      onConnectOrResync: () => load(),
    });
    load();
    return () => {
      if (timer.current) clearTimeout(timer.current);
      unsubscribe();
    };
  }, [load, repoGuid]);

  if (error) {
    return (
      <Text color="fg.error">
        {m.sources_error()}
        {error}
      </Text>
    );
  }
  if (sources === null) {
    return <Text>{m.loading()}</Text>;
  }

  const rulesFor = (s: SnapshotSource) =>
    repo.adoptedSources.filter((r) => ruleMatchesSource(r, s));

  const adoptCell = (s: SnapshotSource) => {
    if (s.planIds.length > 0) {
      return (
        <Text fontSize="xs" color="fg.muted">
          {m.sources_adopt_not_allowed_plan()}
        </Text>
      );
    }
    if (rulesFor(s).length > 0) {
      return <Badge colorPalette="green">{m.sources_adopted()}</Badge>;
    }
    const button = (
      <Button
        size="xs"
        variant="outline"
        disabled={!canAdopt}
        onClick={() =>
          showModal(
            <AdoptSourceModal
              repo={repo}
              hostname={s.hostname}
              paths={s.paths}
            />,
          )
        }
      >
        {m.sources_adopt()}
      </Button>
    );
    return canAdopt ? (
      button
    ) : (
      <Tooltip content={repoModeDisallowedReason(repo.mode)}>{button}</Tooltip>
    );
  };

  return (
    <Stack gap={3}>
      <AdoptionRules repo={repo} canAdopt={canAdopt} />
      <Flex justify="flex-end">
        <Button size="sm" variant="outline" onClick={load}>
          {m.sources_refresh()}
        </Button>
      </Flex>
      {sources.length === 0 ? (
        <Text color="fg.muted">{m.sources_empty()}</Text>
      ) : (
        <Box overflowX="auto">
          <Table.Root size="sm" variant="outline" data-testid="sources-table">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>
                  {m.sources_column_host()}
                </Table.ColumnHeader>
                <Table.ColumnHeader>
                  {m.sources_column_paths()}
                </Table.ColumnHeader>
                <Table.ColumnHeader>
                  {m.sources_column_plans()}
                </Table.ColumnHeader>
                <Table.ColumnHeader textAlign="end">
                  {m.sources_column_snapshots()}
                </Table.ColumnHeader>
                <Table.ColumnHeader>
                  {m.sources_column_last_snapshot()}
                </Table.ColumnHeader>
                <Table.ColumnHeader textAlign="end">
                  {m.sources_column_last_size()}
                </Table.ColumnHeader>
                <Table.ColumnHeader>
                  {m.sources_column_adoption()}
                </Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {sources.map((s) => (
                <Table.Row key={s.id}>
                  <Table.Cell whiteSpace="nowrap">{s.hostname}</Table.Cell>
                  <Table.Cell>
                    <Stack gap={0}>
                      {s.paths.map((p) => (
                        <Text key={p} fontFamily="mono" fontSize="xs">
                          {p}
                        </Text>
                      ))}
                    </Stack>
                  </Table.Cell>
                  <Table.Cell>
                    <Flex gap={1} wrap="wrap">
                      {s.planIds.length === 0 ? (
                        <Badge colorPalette="orange">
                          {m.sources_external()}
                        </Badge>
                      ) : (
                        s.planIds.map((p) => (
                          <Badge key={p} colorPalette="blue">
                            {p}
                          </Badge>
                        ))
                      )}
                    </Flex>
                  </Table.Cell>
                  <Table.Cell textAlign="end">
                    {s.snapshotCount.toString()}
                  </Table.Cell>
                  <Table.Cell whiteSpace="nowrap">
                    {formatTime(Number(s.lastSnapshotTimeMs))}
                  </Table.Cell>
                  <Table.Cell textAlign="end" whiteSpace="nowrap">
                    {s.lastSnapshotBytes > 0
                      ? formatBytes(Number(s.lastSnapshotBytes))
                      : "–"}
                  </Table.Cell>
                  <Table.Cell>{adoptCell(s)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </Box>
      )}
    </Stack>
  );
};

/**
 * AdoptionRules lists the repo's adopted source rules. Removing a rule saves
 * the repo config right away (after confirmation) and stops all management of
 * the sources it matched.
 */
const AdoptionRules = ({
  repo,
  canAdopt,
}: {
  repo: Repo;
  canAdopt: boolean;
}) => {
  const showModal = useShowModal();
  const [, setConfig] = useConfig();

  const remove = async (rule: AdoptedSource) => {
    try {
      setConfig(
        await backrestService.addRepo(
          create(AddRepoRequestSchema, { repo: withoutRule(repo, rule) }),
        ),
      );
      alerts.success(m.sources_rules_removed());
    } catch (e: any) {
      alerts.error(formatErrorAlert(e, m.adopt_modal_error()));
    }
  };

  return (
    <Stack gap={2} data-testid="adoption-rules">
      <Flex justify="space-between" align="center" gap={2}>
        <Text fontWeight="semibold">{m.sources_rules_title()}</Text>
        <Button
          size="sm"
          variant="outline"
          disabled={!canAdopt}
          onClick={() => showModal(<AdoptSourceModal repo={repo} />)}
        >
          {m.sources_rules_add()}
        </Button>
      </Flex>
      <Text fontSize="sm" color="fg.muted">
        {m.sources_rules_help()}
      </Text>
      {repo.adoptedSources.length === 0 ? (
        <Text fontSize="sm" color="fg.muted">
          {m.sources_rules_empty()}
        </Text>
      ) : (
        <Box overflowX="auto">
          <Table.Root size="sm" variant="outline">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>
                  {m.sources_rules_column_selector()}
                </Table.ColumnHeader>
                <Table.ColumnHeader>
                  {m.sources_rules_column_retention()}
                </Table.ColumnHeader>
                <Table.ColumnHeader>
                  {m.sources_rules_column_schedule()}
                </Table.ColumnHeader>
                <Table.ColumnHeader />
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {repo.adoptedSources.map((r) => (
                <Table.Row key={`${r.hostname}|${r.path}`}>
                  <Table.Cell>
                    <Text as="span">{r.hostname}</Text>{" "}
                    <Text as="span" fontFamily="mono" fontSize="xs">
                      {r.path}
                    </Text>
                  </Table.Cell>
                  <Table.Cell>{retentionSummary(r.retention)}</Table.Cell>
                  <Table.Cell>
                    {scheduleEnabled(r.schedule)
                      ? m.sources_rules_schedule_enabled()
                      : m.sources_rules_schedule_disabled()}
                  </Table.Cell>
                  <Table.Cell textAlign="end">
                    <ConfirmButton
                      size="xs"
                      onClickAsync={() => remove(r)}
                      confirmTitle={m.sources_rules_confirm_remove()}
                    >
                      {m.sources_rules_remove()}
                    </ConfirmButton>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </Box>
      )}
    </Stack>
  );
};
