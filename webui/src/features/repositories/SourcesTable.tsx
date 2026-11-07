import React, { useCallback, useEffect, useRef, useState } from "react";
import { Badge, Box, Flex, Stack, Table, Text } from "@chakra-ui/react";
import { create } from "@bufbuild/protobuf";
import {
  ListSourcesRequestSchema,
  SnapshotSource,
} from "../../../gen/ts/v1/service_pb";
import { backrestService } from "../../api/client";
import { operationsStream } from "../../api/oplog";
import { formatBytes, formatTime } from "../../lib/formatting";
import { Button } from "../../components/ui/button";
import * as m from "../../paraglide/messages";

// Reload at most this often while a burst of index operations streams in.
const RELOAD_DEBOUNCE_MS = 500;

/**
 * SourcesTable lists the snapshot sources (host + paths, as restic's
 * `--group-by host,paths`) of a repo, computed by the backend from the
 * indexed snapshots. It reloads when snapshots of the repo are (re)indexed.
 */
export const SourcesTable = ({
  repoId,
  repoGuid,
}: {
  repoId: string;
  repoGuid: string;
}) => {
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

  return (
    <Stack gap={3}>
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
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </Box>
      )}
    </Stack>
  );
};
